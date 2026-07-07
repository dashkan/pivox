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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// --- classifier: resolved-IP guard (DNS-rebinding-safe) -------------------

// TestBlockInternalControl_ClassifiesResolvedAddresses exercises the Control
// hook directly against RESOLVED "ip:port" strings — the exact form the net
// stack hands it at connect time. Because the decision is made on the resolved
// peer IP (not the URL host), a hostname that resolves to an internal address is
// caught here regardless of what the URL looked like — that is the DNS-rebinding
// property. Internal → errBlockedInternalTarget; public → nil.
func TestBlockInternalControl_ClassifiesResolvedAddresses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		address string
		blocked bool
	}{
		{name: "ipv4 loopback", address: "127.0.0.1:80", blocked: true},
		{name: "ipv4 loopback high", address: "127.255.255.254:443", blocked: true},
		{name: "ipv6 loopback", address: "[::1]:80", blocked: true},
		{name: "cloud metadata IP", address: "169.254.169.254:80", blocked: true},
		{name: "link-local unicast v4", address: "169.254.1.1:80", blocked: true},
		{name: "link-local unicast v6", address: "[fe80::1]:80", blocked: true},
		{name: "private 10/8", address: "10.0.0.1:80", blocked: true},
		{name: "private 172.16/12", address: "172.16.5.5:80", blocked: true},
		{name: "private 192.168/16", address: "192.168.1.1:443", blocked: true},
		{name: "ULA fc00::/7", address: "[fc00::1]:80", blocked: true},
		{name: "ULA fd00", address: "[fd12:3456::1]:80", blocked: true},
		{name: "unspecified v4", address: "0.0.0.0:80", blocked: true},
		{name: "unspecified v6", address: "[::]:80", blocked: true},
		{name: "multicast v4", address: "224.0.0.1:80", blocked: true},
		{name: "multicast v6", address: "[ff02::1]:80", blocked: true},
		{name: "cgnat 100.64/10", address: "100.64.1.1:80", blocked: true},
		{name: "cgnat upper", address: "100.127.255.254:80", blocked: true},
		{name: "ipv4-mapped metadata", address: "[::ffff:169.254.169.254]:80", blocked: true},
		{name: "this-host 0.0.0.0/8", address: "0.1.2.3:80", blocked: true},
		{name: "class-E 240/4", address: "240.0.0.1:80", blocked: true},
		{name: "limited broadcast", address: "255.255.255.255:80", blocked: true},
		{name: "nat64 embedding metadata", address: "[64:ff9b::a9fe:a9fe]:80", blocked: true},
		{name: "6to4 embedding metadata", address: "[2002:a9fe:a9fe::1]:443", blocked: true},
		{name: "public v4 google dns", address: "8.8.8.8:80", blocked: false},
		{name: "public v4 cloudflare", address: "1.1.1.1:443", blocked: false},
		{name: "public v6", address: "[2001:4860:4860::8888]:443", blocked: false},
		{name: "cgnat boundary just below", address: "100.63.255.255:80", blocked: false},
		{name: "cgnat boundary just above", address: "100.128.0.0:80", blocked: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := blockInternalControl("tcp", tt.address, nil)
			if tt.blocked {
				require.Error(t, err)
				assert.ErrorIs(t, err, errBlockedInternalTarget)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// TestBlockInternalControl_UnparseableAddressFailsClosed proves a dial address
// the guard can't parse is refused (fail-closed), not waved through.
func TestBlockInternalControl_UnparseableAddressFailsClosed(t *testing.T) {
	t.Parallel()

	err := blockInternalControl("tcp", "not-an-address", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errBlockedInternalTarget)
}

// --- broker: default client egress guard ----------------------------------

// newDefaultClientActivity wires an HTTPActivity whose broker uses its DEFAULT
// HTTP client (no injected client), so the SSRF egress guard is in force per
// allowInternal. This is the shape that exercises the guard — the other broker
// tests inject srv.Client(), which is used as-is and bypasses the guard.
func newDefaultClientActivity(t *testing.T, store Store, enc crypto.Encryptor, allowInternal bool, sleep engine.SleepFunc) *HTTPActivity {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	broker := NewBroker(Config{
		Queries:               store,
		Encryptor:             enc,
		AllowInternalNetworks: allowInternal,
		Logger:                logger,
	})
	eval, err := engine.NewEvaluator()
	require.NoError(t, err)
	return NewHTTPActivity(ActivityConfig{Evaluator: eval, Broker: broker, Store: store, Sleep: sleep})
}

// TestBroker_Do_BlocksInternalTargetTerminally is the core guard assertion: with
// the guard on (AllowInternalNetworks:false), a connector pointed at the loopback
// test server is blocked at connect time, the run-facing error is TERMINAL (not a
// *transientError, not a job retry), and the retry loop makes no in-process
// attempts — the server is never reached.
func TestBroker_Do_BlocksInternalTargetTerminally(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()

	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	conn, connName := makeConnector(t, org, pgtype.UUID{}, srv.URL, nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	sleeper := &recordingSleeper{}
	// A generous retry policy would retry a *transientError; a blocked target must
	// NOT be retried, so this proves the terminal classification through the loop.
	act := newDefaultClientActivity(t, store, enc, false, sleeper.sleep)

	step := httpStep(connName, "GET", func(h *workflowsv1.HttpActivity) {
		h.Retry = &workflowsv1.RetryPolicy{MaxAttempts: 3, InitialBackoff: durationpb.New(time.Millisecond)}
	})
	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, step)

	require.Error(t, err)
	assert.ErrorIs(t, err, errBlockedInternalTarget, "error identifies the internal-target block")
	assert.False(t, engine.IsRetryable(err), "a blocked internal target must not be a job retry")

	var te *transientError
	assert.False(t, errors.As(err, &te), "a blocked internal target must be TERMINAL, not a *transientError")

	assert.Equal(t, int32(0), count.Load(), "the loopback server must never be reached")
	assert.Empty(t, sleeper.waits, "a terminal block must not trigger in-process retries")
}

// TestBroker_Do_AllowsInternalWhenFlagSet proves the on-prem escape hatch: the
// SAME loopback connector succeeds when AllowInternalNetworks is true.
func TestBroker_Do_AllowsInternalWhenFlagSet(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()

	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	conn, connName := makeConnector(t, org, pgtype.UUID{}, srv.URL, nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	act := newDefaultClientActivity(t, store, enc, true, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	out, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))
	require.NoError(t, err)

	m := out.(map[string]any)
	assert.Equal(t, int64(200), m["status"])
	assert.Equal(t, `{"ok":true}`, m["body"])
	assert.Equal(t, int32(1), count.Load(), "the request reaches the server when internal is allowed")
}

// TestBroker_Do_HostnameResolvingToLoopbackBlocked proves the resolved-IP /
// DNS-rebinding property end-to-end: the connector base_url uses the hostname
// "localhost" (not an IP literal), yet the guard blocks it because localhost
// RESOLVES to a loopback address at connect time.
func TestBroker_Do_HostnameResolvingToLoopbackBlocked(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Rewrite the 127.0.0.1 host to the hostname "localhost" — same port, same
	// server, but now the URL host is a name the stack must resolve.
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	baseURL := fmt.Sprintf("http://localhost:%s", u.Port())

	conn, connName := makeConnector(t, org, pgtype.UUID{}, baseURL, nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	act := newDefaultClientActivity(t, store, enc, false, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err = act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))

	require.Error(t, err)
	assert.ErrorIs(t, err, errBlockedInternalTarget, "a hostname resolving to loopback is blocked at connect time")
	assert.False(t, engine.IsRetryable(err))
}

// TestBroker_Do_DoesNotFollowRedirect proves the credential-leak fix: the broker
// does not follow a 3xx, so an injected credential header is never re-sent to a
// redirect target the operator never configured. The target returns 200 — had the
// broker followed the redirect, the activity would see a 200 success; a non-nil
// error (the raw 302, classified terminal) plus a never-hit target proves it.
func TestBroker_Do_DoesNotFollowRedirect(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	org := uuid.New()

	var targetHit atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHit.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound) // 302
	}))
	defer redirector.Close()

	conn, connName := makeConnector(t, org, pgtype.UUID{}, redirector.URL, nil)
	store := &fakeStore{connectors: map[uuid.UUID]db.Connector{conn.ID: conn}}
	// allowInternal:true so the loopback test servers are reachable; the default
	// client (not injected) carries the no-follow CheckRedirect.
	act := newDefaultClientActivity(t, store, enc, true, nil)

	rc := engine.NewRunContext(engine.RunContextConfig{OrgID: org})
	_, err := act.Execute(context.Background(), rc, httpStep(connName, "GET", nil))

	require.Error(t, err, "an unfollowed 302 is a terminal (non-2xx) outcome, not a success")
	assert.Equal(t, int32(0), targetHit.Load(), "the redirect target must never be reached — the credential can't leak to it")
}

// TestNewBroker_ClampsMaxResponseBytes proves the operator-supplied response cap
// is clamped into [512 KiB, 10 MiB] and defaults to 1 MiB when unset.
func TestNewBroker_ClampsMaxResponseBytes(t *testing.T) {
	t.Parallel()

	enc := testEncryptor(t)
	store := &fakeStore{}
	cases := []struct {
		name string
		in   int64
		want int64
	}{
		{"unset defaults to 1 MiB", 0, 1 << 20},
		{"below floor clamps up", 100, 512 << 10},
		{"512 KiB floor", 512 << 10, 512 << 10},
		{"in band unchanged", 2 << 20, 2 << 20},
		{"above ceiling clamps down", 50 << 20, 10 << 20},
		{"10 MiB ceiling", 10 << 20, 10 << 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := NewBroker(Config{Queries: store, Encryptor: enc, MaxResponseBytes: tc.in})
			assert.Equal(t, tc.want, b.maxResponseBytes)
		})
	}
}
