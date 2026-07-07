package connector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// --- helpers --------------------------------------------------------------

func testEncryptor(t *testing.T) crypto.Encryptor {
	t.Helper()
	ks, err := crypto.GenerateCleartextKeyset()
	require.NoError(t, err)
	enc, err := crypto.NewLocalEncryptor(ks)
	require.NoError(t, err)
	return enc
}

// fakeStore is a behavioral fake of the two-method [Store] port. It is NOT a
// mocked db.Querier — it returns real rows and honors an injected DB fault.
type fakeStore struct {
	secrets      map[uuid.UUID]db.Secret
	connectors   map[uuid.UUID]db.Connector
	secretErr    error
	connectorErr error
}

func (s *fakeStore) GetSecret(_ context.Context, id uuid.UUID) (db.Secret, error) {
	if s.secretErr != nil {
		return db.Secret{}, s.secretErr
	}
	row, ok := s.secrets[id]
	if !ok {
		return db.Secret{}, pgx.ErrNoRows
	}
	return row, nil
}

func (s *fakeStore) GetConnector(_ context.Context, id uuid.UUID) (db.Connector, error) {
	if s.connectorErr != nil {
		return db.Connector{}, s.connectorErr
	}
	row, ok := s.connectors[id]
	if !ok {
		return db.Connector{}, pgx.ErrNoRows
	}
	return row, nil
}

// makeSecret builds an encrypted Secret row (real crypto, real AAD) and its
// resource name. space is optional (pgtype.UUID{} = org-scoped).
func makeSecret(t *testing.T, enc crypto.Encryptor, org uuid.UUID, space pgtype.UUID, plaintext string) (db.Secret, string) {
	t.Helper()
	id := uuid.New()
	ct, err := enc.Encrypt([]byte(plaintext), secretAAD(id))
	require.NoError(t, err)
	return db.Secret{ID: id, OrgID: org, SpaceID: space, ValueCiphertext: ct},
		fmt.Sprintf("organizations/acme/secrets/%s", id)
}

// makeConnector builds an http Connector row (config marshaled as protojson,
// symmetric with the connectors service) and its resource name.
func makeConnector(t *testing.T, org uuid.UUID, space pgtype.UUID, baseURL string, headers map[string]string) (db.Connector, string) {
	t.Helper()
	id := uuid.New()
	cfg, err := protojson.Marshal(&workflowsv1.Connector{
		Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
			BaseUrl: baseURL,
			Headers: headers,
		}},
	})
	require.NoError(t, err)
	return db.Connector{ID: id, OrgID: org, SpaceID: space, Config: cfg},
		fmt.Sprintf("organizations/acme/connectors/%s", id)
}

func httpStep(connectorName, method string, opts func(*workflowsv1.HttpActivity)) *workflowsv1.Step {
	h := &workflowsv1.HttpActivity{Connector: connectorName, Method: method}
	if opts != nil {
		opts(h)
	}
	return &workflowsv1.Step{
		Id: "call",
		Kind: &workflowsv1.Step_Activity{
			Activity: &workflowsv1.Activity{Kind: &workflowsv1.Activity_Http{Http: h}},
		},
	}
}

// recordingSleeper captures backoff durations without sleeping. The retry loop
// is sequential, so no synchronization is needed.
type recordingSleeper struct{ waits []time.Duration }

func (s *recordingSleeper) sleep(_ context.Context, d time.Duration) error {
	s.waits = append(s.waits, d)
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// headerCapture records request headers race-safely.
type headerCapture struct {
	mu sync.Mutex
	h  http.Header
}

func (c *headerCapture) record(h http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.h = h.Clone()
}

func (c *headerCapture) get(name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.h.Get(name)
}

// newActivity wires an HTTPActivity over the given store + http client + sleeper.
func newActivity(t *testing.T, store Store, enc crypto.Encryptor, client *http.Client, logger *slog.Logger, sleep engine.SleepFunc) *HTTPActivity {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	}
	broker := NewBroker(Config{Queries: store, Encryptor: enc, HTTPClient: client, Logger: logger})
	eval, err := engine.NewEvaluator()
	require.NoError(t, err)
	return NewHTTPActivity(ActivityConfig{Evaluator: eval, Broker: broker, Store: store, Sleep: sleep})
}

// --- CEL fence ------------------------------------------------------------

func TestConnectorEnv_HasSecretButNoRunRoots(t *testing.T) {
	t.Parallel()

	env, err := buildConnectorEnv(func(string) (string, error) { return "v", nil })
	require.NoError(t, err)

	// secret() is live in the connector-config env.
	_, iss := env.Compile(`secret("organizations/a/secrets/x")`)
	assert.True(t, iss == nil || iss.Err() == nil, "secret() must compile in the connector env")

	// The run-context roots do NOT exist here — a connector header can't read
	// trigger/params/steps/vars. This is the other half of the fence (the
	// run-context env rejecting secret() is proved in internal/engine).
	for _, expr := range []string{`trigger.x`, `params.x`, `steps.a.output`, `vars.x`} {
		_, iss := env.Compile(expr)
		assert.True(t, iss != nil && iss.Err() != nil, "%q must fail to compile in the connector env", expr)
	}
}

// --- secret injection + redaction -----------------------------------------

func TestHTTPActivity_SecretInjectedAndNeverLeaked(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	const plaintext = "s3cr3t-token-42"

	cap := &headerCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r.Header)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	secret, secretName := makeSecret(t, enc, org, pgtype.UUID{}, plaintext)
	conn, connName := makeConnector(t, org, pgtype.UUID{}, srv.URL, map[string]string{
		"Authorization": fmt.Sprintf(`"Bearer " + secret(%q)`, secretName),
	})
	store := &fakeStore{
		secrets:    map[uuid.UUID]db.Secret{secret.ID: secret},
		connectors: map[uuid.UUID]db.Connector{conn.ID: conn},
	}

	var logbuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logbuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	act := newActivity(t, store, enc, srv.Client(), logger, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	out, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))
	require.NoError(t, err)

	// The server saw the decrypted secret in the header.
	assert.Equal(t, "Bearer "+plaintext, cap.get("Authorization"))

	m, ok := out.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, int64(200), m["status"])

	// The plaintext never appears in the logs or the step output.
	assert.NotContains(t, logbuf.String(), plaintext, "secret must not be logged")
	assert.NotContains(t, fmt.Sprint(out), plaintext, "secret must not be in the step output")
	assert.NotEmpty(t, logbuf.String(), "the broker should have logged the (redacted) request")
}

// --- 2xx success ----------------------------------------------------------

func TestHTTPActivity_2xxOutput(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "abc")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer srv.Close()

	conn, connName := makeConnector(t, org, pgtype.UUID{}, srv.URL, nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	act := newActivity(t, store, enc, srv.Client(), nil, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	out, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))
	require.NoError(t, err)

	m := out.(map[string]any)
	assert.Equal(t, int64(200), m["status"])
	assert.Equal(t, `{"hello":"world"}`, m["body"])
	headers := m["headers"].(map[string]any)
	assert.Equal(t, "abc", headers["X-Custom"])
}

// --- retry: 5xx exhausts to a terminal error ------------------------------

func TestHTTPActivity_5xxRetriesThenTerminal(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()

	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`upstream down`))
	}))
	defer srv.Close()

	conn, connName := makeConnector(t, org, pgtype.UUID{}, srv.URL, nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	sleeper := &recordingSleeper{}
	act := newActivity(t, store, enc, srv.Client(), nil, sleeper.sleep)

	step := httpStep(connName, "GET", func(h *workflowsv1.HttpActivity) {
		h.Retry = &workflowsv1.RetryPolicy{
			MaxAttempts:       3,
			InitialBackoff:    durationpb.New(10 * time.Millisecond),
			MaxBackoff:        durationpb.New(time.Second),
			BackoffMultiplier: 2,
		}
	})
	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, step)

	require.Error(t, err)
	// Exhausted transient → TERMINAL, not a job retry.
	assert.False(t, engine.IsRetryable(err), "exhausted http retry must not be an engine.RetryableError")
	assert.Equal(t, int32(3), count.Load(), "all attempts made")
	assert.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, sleeper.waits)

	// The terminal error carries status + body for a future catch.
	var re *ResponseError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, 503, re.Status)
	assert.Equal(t, "upstream down", string(re.Body))
}

// --- 404 default terminal; success_status flips it to success -------------

func TestHTTPActivity_404TerminalByDefault(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	conn, connName := makeConnector(t, org, pgtype.UUID{}, srv.URL, nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	act := newActivity(t, store, enc, srv.Client(), nil, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))

	require.Error(t, err)
	assert.False(t, engine.IsRetryable(err))
	var re *ResponseError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, 404, re.Status)
	assert.Equal(t, int32(1), count.Load(), "no retry on a terminal status")
}

func TestHTTPActivity_404AsSuccessStatus(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`missing`))
	}))
	defer srv.Close()

	conn, connName := makeConnector(t, org, pgtype.UUID{}, srv.URL, nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	act := newActivity(t, store, enc, srv.Client(), nil, nil)

	step := httpStep(connName, "GET", func(h *workflowsv1.HttpActivity) {
		h.SuccessStatus = []int32{404}
	})
	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	out, err := act.Execute(context.Background(), rc, step)
	require.NoError(t, err)

	m := out.(map[string]any)
	assert.Equal(t, int64(404), m["status"])
	assert.Equal(t, "missing", m["body"])
}

// --- 429 in retryable_status retries --------------------------------------

func TestHTTPActivity_429RetryableStatusRetries(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	conn, connName := makeConnector(t, org, pgtype.UUID{}, srv.URL, nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	sleeper := &recordingSleeper{}
	act := newActivity(t, store, enc, srv.Client(), nil, sleeper.sleep)

	step := httpStep(connName, "GET", func(h *workflowsv1.HttpActivity) {
		h.RetryableStatus = []int32{429}
		h.Retry = &workflowsv1.RetryPolicy{MaxAttempts: 2, InitialBackoff: durationpb.New(time.Millisecond)}
	})
	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, step)

	require.Error(t, err)
	assert.False(t, engine.IsRetryable(err))
	assert.Equal(t, int32(2), count.Load())
	assert.Len(t, sleeper.waits, 1)
	var re *ResponseError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, 429, re.Status)
}

// --- network failure retries ----------------------------------------------

func TestHTTPActivity_NetworkFailureRetries(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	var count atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		count.Add(1)
		return nil, errors.New("dial tcp: connection refused")
	})}

	conn, connName := makeConnector(t, org, pgtype.UUID{}, "http://unreachable.invalid", nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	sleeper := &recordingSleeper{}
	act := newActivity(t, store, enc, client, nil, sleeper.sleep)

	step := httpStep(connName, "GET", func(h *workflowsv1.HttpActivity) {
		h.Retry = &workflowsv1.RetryPolicy{MaxAttempts: 3, InitialBackoff: durationpb.New(time.Millisecond)}
	})
	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, step)

	require.Error(t, err)
	// A network failure retried to exhaustion is terminal, not a job retry.
	assert.False(t, engine.IsRetryable(err))
	assert.Equal(t, int32(3), count.Load())
	assert.Len(t, sleeper.waits, 2)
}

// --- header precedence: connector wins ------------------------------------

func TestHTTPActivity_ConnectorHeaderWins(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	cap := &headerCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r.Header)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	conn, connName := makeConnector(t, org, pgtype.UUID{}, srv.URL, map[string]string{
		"X-Test": `"connector-value"`,
	})
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	act := newActivity(t, store, enc, srv.Client(), nil, nil)

	step := httpStep(connName, "GET", func(h *workflowsv1.HttpActivity) {
		h.Headers = map[string]string{"X-Test": `"activity-value"`}
	})
	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, step)
	require.NoError(t, err)

	assert.Equal(t, "connector-value", cap.get("X-Test"), "connector header overrides the activity header")
}

// --- infra faults escalate to a job retry ---------------------------------

func TestHTTPActivity_ConnectorDBFaultIsRetryable(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	conn, connName := makeConnector(t, org, pgtype.UUID{}, "http://x.invalid", nil)
	store := &fakeStore{
		connectors:   map[uuid.UUID]db.Connector{conn.ID: conn},
		connectorErr: errors.New("connection pool exhausted"),
	}
	act := newActivity(t, store, enc, &http.Client{}, nil, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))

	require.Error(t, err)
	assert.True(t, engine.IsRetryable(err), "a DB fault resolving the connector must be a job retry")
}

func TestHTTPActivity_SecretDBFaultIsRetryable(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	secret, secretName := makeSecret(t, enc, org, pgtype.UUID{}, "v")
	conn, connName := makeConnector(t, org, pgtype.UUID{}, "http://x.invalid", map[string]string{
		"Authorization": fmt.Sprintf(`secret(%q)`, secretName),
	})
	store := &fakeStore{
		connectors: map[uuid.UUID]db.Connector{conn.ID: conn},
		secrets:    map[uuid.UUID]db.Secret{secret.ID: secret},
		secretErr:  errors.New("db down"),
	}
	act := newActivity(t, store, enc, &http.Client{}, nil, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))

	require.Error(t, err)
	assert.True(t, engine.IsRetryable(err), "a DB fault resolving a secret must be a job retry")
}

// --- missing / out-of-scope secrets are terminal --------------------------

func TestHTTPActivity_MissingSecretTerminal(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	// Reference a secret uuid that isn't in the store.
	missing := fmt.Sprintf("organizations/acme/secrets/%s", uuid.New())
	conn, connName := makeConnector(t, org, pgtype.UUID{}, "http://x.invalid", map[string]string{
		"Authorization": fmt.Sprintf(`secret(%q)`, missing),
	})
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	act := newActivity(t, store, enc, &http.Client{}, nil, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))

	require.Error(t, err)
	assert.False(t, engine.IsRetryable(err), "a missing secret is terminal, not a job retry")
}

func TestHTTPActivity_OutOfScopeSecretTerminal(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()
	otherOrg := uuid.New()
	// Secret belongs to a DIFFERENT org than the connector.
	secret, secretName := makeSecret(t, enc, otherOrg, pgtype.UUID{}, "v")
	conn, connName := makeConnector(t, org, pgtype.UUID{}, "http://x.invalid", map[string]string{
		"Authorization": fmt.Sprintf(`secret(%q)`, secretName),
	})
	store := &fakeStore{
		connectors: map[uuid.UUID]db.Connector{conn.ID: conn},
		secrets:    map[uuid.UUID]db.Secret{secret.ID: secret},
	}
	act := newActivity(t, store, enc, &http.Client{}, nil, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))

	require.Error(t, err)
	assert.False(t, engine.IsRetryable(err), "an out-of-scope secret is terminal")
}

// --- connector out of the run's scope is terminal -------------------------

func TestHTTPActivity_ConnectorOutOfRunScopeNotFound(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	connOrg := uuid.New()
	runOrg := uuid.New() // run is a DIFFERENT org than the connector
	conn, connName := makeConnector(t, connOrg, pgtype.UUID{}, "http://x.invalid", nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	act := newActivity(t, store, enc, &http.Client{}, nil, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: runOrg})
	_, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))

	require.Error(t, err)
	assert.False(t, engine.IsRetryable(err))
	assert.Contains(t, err.Error(), "not found")
}

// --- buildURL keeps the host fixed ----------------------------------------

func TestBuildURL_PathCannotChangeHost(t *testing.T) {
	t.Parallel()

	got, err := buildURL("https://api.example.com/base", "//evil.com/steal", nil)
	require.NoError(t, err)
	parsed, err := url.Parse(got)
	require.NoError(t, err)
	// The path may contain the attacker text as a segment, but the HOST — the
	// origin the credentialed request is sent to — stays the connector's.
	assert.Equal(t, "api.example.com", parsed.Host, "host stays the connector's")
	assert.Equal(t, "https", parsed.Scheme)
}
