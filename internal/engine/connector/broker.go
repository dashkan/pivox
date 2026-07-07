package connector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

const (
	// defaultMaxResponseBytes caps how much of a response body the broker reads
	// when Config.MaxResponseBytes is unset, so a hostile or misbehaving upstream
	// can't OOM the worker. Overridable via --workflow-http-max-response-size.
	defaultMaxResponseBytes = 1 << 20 // 1 MiB

	// minResponseBytes / maxResponseBytesCap bound an operator-configured cap so
	// it can be neither uselessly small nor large enough to threaten the worker.
	minResponseBytes    = 512 << 10 // 512 KiB
	maxResponseBytesCap = 10 << 20  // 10 MiB

	// defaultHTTPTimeout bounds a single outbound request when the caller does
	// not supply its own *http.Client. ctx cancellation still applies on top.
	defaultHTTPTimeout = 30 * time.Second
)

// decryptor is the narrow slice of [crypto.Encryptor] the broker uses. The full
// Encryptor satisfies it; the broker never encrypts.
type decryptor interface {
	Decrypt(ciphertext, aad []byte) ([]byte, error)
}

// crypto.Encryptor is the production decryptor.
var _ decryptor = (crypto.Encryptor)(nil)

// errConnectorConfig is the generic TERMINAL error surfaced to a run when a
// connector's config fails to evaluate — a bad header expression, a missing or
// out-of-scope secret, or a non-string header. The specific cause (which can
// name the secrets a connector references) is logged server-side, never
// persisted to the run's error where a lower-privileged run-reader could see it.
var errConnectorConfig = errors.New("connector configuration error")

// Broker is the single secret-injecting execution path. Given a resolved
// Connector and an already-run-context-evaluated request, it evaluates the
// connector's headers in the connector-config env (secret() live), merges them
// over the activity headers (connector WINS), performs the call with a
// ctx-aware client, and returns the raw response. Secrets are decrypted and
// injected here and never escape into the run context or a log.
//
// Broker is safe for concurrent use: Do holds no shared mutable state — each
// call builds its own connector-config env closing over that call's connector
// scope and context.
type Broker struct {
	queries          Store
	encryptor        decryptor
	httpClient       *http.Client
	maxResponseBytes int64
	logger           *slog.Logger
}

// Config configures a [Broker]. Queries and Encryptor are required; HTTPClient
// and Logger default.
type Config struct {
	// Queries reads Secrets (and, for the activity, Connectors). Required.
	Queries Store
	// Encryptor decrypts secret values at rest. Required.
	Encryptor crypto.Encryptor
	// HTTPClient performs outbound requests. Optional; when nil a default client
	// (30s timeout, response cap, SSRF egress guard) is built per
	// AllowInternalNetworks. An injected client is used AS-IS — the caller owns
	// its dialer and its egress policy.
	HTTPClient *http.Client
	// AllowInternalNetworks controls the SSRF egress guard on the default HTTP
	// client. When false (the default, REQUIRED for shared multi-tenant cloud)
	// the client refuses connections whose RESOLVED peer IP is internal
	// (loopback, link-local/metadata, private, CGNAT). When true (single-tenant
	// on-prem) the client dials any address. Ignored when HTTPClient is set.
	AllowInternalNetworks bool
	// MaxResponseBytes caps how much of a response body the broker reads, so a
	// hostile upstream can't OOM the worker. Optional; defaults to 1 MiB.
	MaxResponseBytes int64
	// Logger is the structured logger. Optional; slog.Default() when nil.
	Logger *slog.Logger
}

// NewBroker builds a Broker from cfg. It panics on a missing required field —
// a startup-time programmer error, per the repo constructor convention.
func NewBroker(cfg Config) *Broker {
	if cfg.Queries == nil {
		panic("connector: Config.Queries is required")
	}
	if cfg.Encryptor == nil {
		panic("connector: Config.Encryptor is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = newHTTPClient(cfg.AllowInternalNetworks)
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	maxBytes := cfg.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxResponseBytes
	}
	// Clamp an operator-supplied cap into a sane band [512 KiB, 10 MiB].
	maxBytes = min(max(maxBytes, minResponseBytes), maxResponseBytesCap)
	return &Broker{
		queries:          cfg.Queries,
		encryptor:        cfg.Encryptor,
		httpClient:       client,
		maxResponseBytes: maxBytes,
		logger:           logger,
	}
}

// Request is one HTTP call, with every CEL input already evaluated by the http
// activity against the run-context env. The broker adds only the connector's
// base URL and credentialed headers.
type Request struct {
	// Method is the HTTP method (GET, POST, ...).
	Method string
	// Path is appended to the connector's base URL.
	Path string
	// Query is the URL query parameters.
	Query map[string]string
	// Headers are the activity's headers; a connector header of the same name
	// overrides them.
	Headers map[string]string
	// Body is the raw request body (empty for none).
	Body []byte
}

// Response is a completed HTTP round-trip. Status/Headers/Body are the raw
// outcome; the http activity classifies the status into success/retry/terminal.
type Response struct {
	Status  int
	Headers http.Header
	Body    []byte
}

// Do resolves conn's credentialed config, builds and performs the request, and
// returns the raw response. Error taxonomy:
//
//   - nil error with a non-nil Response — a round-trip completed (any status).
//   - [engine.Retryable] — an infra fault (DB blip resolving a secret): the run
//     job should retry.
//   - *transientError — a network/transport failure: the http activity retries
//     it in-process.
//   - any other (plain) error — terminal config/secret error (non-http
//     connector, bad base URL, missing/out-of-scope secret, decrypt failure).
func (b *Broker) Do(ctx context.Context, conn db.Connector, req Request) (*Response, error) {
	httpCfg, err := parseHTTPConnector(conn)
	if err != nil {
		return nil, err // terminal: not an http connector / bad config
	}

	connHeaders, err := b.evalConnectorHeaders(ctx, conn, httpCfg)
	if err != nil {
		return nil, err // terminal or engine.Retryable (secret resolution)
	}

	reqURL, err := buildURL(httpCfg.GetBaseUrl(), req.Path, req.Query)
	if err != nil {
		return nil, err // terminal: bad base URL / path
	}

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("connector: build request: %w", err) // terminal: bad method/url
	}
	// Activity headers first, then connector headers OVERRIDE them — an activity
	// can't override a connector's credentialed header.
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range connHeaders {
		httpReq.Header.Set(k, v)
	}

	b.logger.DebugContext(ctx, "connector: outbound request",
		"method", httpReq.Method, "url", redactURL(reqURL), "connector_id", conn.ID)

	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, errBlockedInternalTarget) {
			// The connector's target RESOLVED to an internal address (the SSRF
			// egress guard tripped at connect time). Retrying never unblocks it,
			// so this is TERMINAL — NOT a *transientError.
			return nil, fmt.Errorf("connector: target is an internal network address, which is not allowed: %w", err)
		}
		// Transport/network failure — the activity retries it in-process.
		return nil, &transientError{cause: fmt.Errorf("connector: request failed: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, b.maxResponseBytes))
	if err != nil {
		// A truncated/failed body read is a transient transport condition.
		return nil, &transientError{cause: fmt.Errorf("connector: read response body: %w", err)}
	}

	b.logger.DebugContext(ctx, "connector: response",
		"status", resp.StatusCode, "connector_id", conn.ID)

	return &Response{
		Status:  resp.StatusCode,
		Headers: resp.Header.Clone(),
		Body:    bodyBytes,
	}, nil
}

// evalConnectorHeaders evaluates each connector header value in the
// connector-config env (secret() live) and returns the resolved name→value map,
// with any secret decrypted and injected. A DB fault surfaces as
// [engine.Retryable]; a bad expression or secret error is terminal.
func (b *Broker) evalConnectorHeaders(ctx context.Context, conn db.Connector, httpCfg *workflowsv1.HttpConnector) (map[string]string, error) {
	headers := httpCfg.GetHeaders()
	if len(headers) == 0 {
		return nil, nil
	}

	var res secretResolution
	env, err := buildConnectorEnv(func(name string) (string, error) {
		v, rerr := resolveSecret(ctx, b.queries, b.encryptor, name, conn.OrgID, conn.SpaceID)
		if rerr != nil {
			if res.err == nil {
				res.err = rerr // capture the first real error with its classification
			}
			return "", rerr
		}
		return v, nil
	})
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(headers))
	for name, expr := range headers {
		// Every failure below is logged server-side with its detail (the header
		// expression embeds the referenced secret's resource name) but returns the
		// generic errConnectorConfig, so the run's error never discloses which
		// secrets a connector references to a run-reader.
		ast, iss := env.Compile(expr)
		if iss != nil && iss.Err() != nil {
			// A connector header referencing a run-context root, or otherwise
			// malformed, fails to compile here — terminal (bad config).
			b.logger.WarnContext(ctx, "connector: header expression failed to compile",
				"connector_id", conn.ID, "header", name, "error", iss.Err())
			return nil, errConnectorConfig
		}
		prg, err := env.Program(ast)
		if err != nil {
			b.logger.WarnContext(ctx, "connector: header program build failed",
				"connector_id", conn.ID, "header", name, "error", err)
			return nil, errConnectorConfig
		}
		val, _, evalErr := prg.ContextEval(ctx, cel.NoVars())
		if res.err != nil {
			// A secret() failure. A retryable (infra) error propagates for a whole-
			// job retry — its detail goes to worker logs, not the run's error. A
			// terminal secret error (missing/out-of-scope/decrypt) would embed the
			// secret's resource name into the run error, so log it and surface the
			// generic terminal error instead.
			if engine.IsRetryable(res.err) {
				return nil, res.err
			}
			b.logger.WarnContext(ctx, "connector: secret resolution failed",
				"connector_id", conn.ID, "header", name, "error", res.err)
			return nil, errConnectorConfig
		}
		if evalErr != nil {
			b.logger.WarnContext(ctx, "connector: header evaluation failed",
				"connector_id", conn.ID, "header", name, "error", evalErr)
			return nil, errConnectorConfig
		}
		s, ok := val.Value().(string)
		if !ok {
			b.logger.WarnContext(ctx, "connector: header did not evaluate to a string",
				"connector_id", conn.ID, "header", name, "type", fmt.Sprintf("%T", val.Value()))
			return nil, errConnectorConfig
		}
		out[name] = s
	}
	return out, nil
}

// parseHTTPConnector lifts the typed HttpConnector out of a connector's config
// JSONB (persisted protojson, symmetric with convert.ConnectorToProto). A
// connector that is not an http connector is a terminal error.
func parseHTTPConnector(conn db.Connector) (*workflowsv1.HttpConnector, error) {
	if len(conn.Config) == 0 {
		return nil, fmt.Errorf("connector %s: has no config", conn.ID)
	}
	var scratch workflowsv1.Connector
	if err := protojson.Unmarshal(conn.Config, &scratch); err != nil {
		return nil, fmt.Errorf("connector %s: unmarshal config: %w", conn.ID, err)
	}
	httpCfg := scratch.GetHttp()
	if httpCfg == nil {
		return nil, fmt.Errorf("connector %s: is not an http connector", conn.ID)
	}
	if httpCfg.GetBaseUrl() == "" {
		return nil, fmt.Errorf("connector %s: http connector has no base_url", conn.ID)
	}
	return httpCfg, nil
}

// buildURL joins the connector's base URL, the activity path, and the query
// map. The path is appended to the base URL's path — it can NOT change the host
// or scheme, so a hostile path expression can't redirect the credentialed
// request to a different origin.
func buildURL(baseURL, path string, query map[string]string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("connector: invalid base_url %q: %w", baseURL, err)
	}
	if base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("connector: base_url %q must be an absolute URL", baseURL)
	}
	if path != "" {
		base.Path = joinPath(base.Path, path)
	}
	if len(query) > 0 {
		q := base.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		base.RawQuery = q.Encode()
	}
	return base.String(), nil
}

// joinPath appends rel to base with exactly one separating slash.
func joinPath(base, rel string) string {
	if base == "" {
		return rel
	}
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(rel, "/")
}

// redactURL renders a URL for logging with its query string stripped — query
// values are run-context CEL output and may carry sensitive data. Host and path
// are safe operational context. Header values (which carry injected secrets) are
// never logged at all.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable-url>"
	}
	u.RawQuery = ""
	u.User = nil
	return u.String()
}
