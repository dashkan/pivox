package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// HTTPActivity is the `http` activity: it resolves the connector, evaluates its
// own inputs (path, query, headers, body) in the run-context env, and drives the
// [Broker] under an in-process retry loop. It never sees plaintext secrets —
// those are resolved and injected inside the broker.
//
// Retry semantics (the crux):
//
//   - A transient outcome (network failure, 5xx, or a code in retryable_status)
//     retries in-process per the activity's RetryPolicy. On exhaustion it fails
//     the run TERMINALLY — it does NOT hand the job back to River.
//   - An infra fault (a DB blip resolving the connector or a secret) surfaces as
//     [engine.Retryable], which the worker escalates to a whole-job retry.
//   - A non-success, non-retryable status (e.g. a 404 without success_status) is
//     a terminal [*ResponseError] carrying status + body for a future catch.
type HTTPActivity struct {
	eval   *engine.Evaluator
	broker *Broker
	store  Store
	sleep  engine.SleepFunc
}

var _ engine.Activity = (*HTTPActivity)(nil)

// ActivityConfig configures an [HTTPActivity].
type ActivityConfig struct {
	// Evaluator is the shared run-context CEL evaluator (no secret()). Required.
	Evaluator *engine.Evaluator
	// Broker performs the secret-injecting call. Required.
	Broker *Broker
	// Store resolves the connector referenced by the activity. Required.
	Store Store
	// Sleep is the retry backoff sleeper. Optional; a real ctx-aware sleep is
	// used when nil. Tests inject a fake so retries don't sleep real time.
	Sleep engine.SleepFunc
}

// NewHTTPActivity builds an HTTPActivity. It panics on a missing required
// dependency, per the repo constructor convention.
func NewHTTPActivity(cfg ActivityConfig) *HTTPActivity {
	if cfg.Evaluator == nil {
		panic("connector: ActivityConfig.Evaluator is required")
	}
	if cfg.Broker == nil {
		panic("connector: ActivityConfig.Broker is required")
	}
	if cfg.Store == nil {
		panic("connector: ActivityConfig.Store is required")
	}
	return &HTTPActivity{
		eval:   cfg.Evaluator,
		broker: cfg.Broker,
		store:  cfg.Store,
		sleep:  cfg.Sleep,
	}
}

// Execute implements [engine.Activity].
func (a *HTTPActivity) Execute(ctx context.Context, rc *engine.RunContext, step *workflowsv1.Step) (any, error) {
	spec := step.GetActivity().GetHttp()
	if spec == nil {
		return nil, fmt.Errorf("connector: step %q is not an http activity", step.GetId())
	}

	// Resolve + scope-check the connector once, outside the retry loop: a DB
	// fault here is infra (job retry), and the result is stable across attempts.
	conn, err := a.resolveConnector(ctx, rc, spec.GetConnector())
	if err != nil {
		return nil, err
	}

	// Evaluate the activity's own inputs in the run-context env (no secret()).
	// Every CEL error here is terminal — a bad expression won't heal on retry.
	req, err := a.buildRequest(ctx, rc, spec)
	if err != nil {
		return nil, err
	}

	policy := retryPolicy(spec.GetRetry())
	resp, err := engine.WithRetry(ctx, policy, a.sleep, isTransient,
		func(ctx context.Context) (*Response, error) {
			r, err := a.broker.Do(ctx, conn, req)
			if err != nil {
				// Pass errors through unchanged: engine.Retryable (infra —
				// WithRetry won't retry it), *transientError (network — retried),
				// or a plain terminal config/secret error.
				return nil, err
			}
			switch {
			case isSuccess(r.Status, spec.GetSuccessStatus()):
				return r, nil
			case isRetryableStatus(r.Status, spec.GetRetryableStatus()):
				return nil, &transientError{cause: newResponseError(r)}
			default:
				return nil, newResponseError(r) // terminal non-success
			}
		})
	if err != nil {
		// engine.Retryable → the worker re-runs the job; anything else (exhausted
		// transient, terminal ResponseError, config error) fails the run.
		return nil, err
	}
	return httpOutput(resp), nil
}

// resolveConnector loads the connector named by ref and confirms it belongs to
// the run's scope. A DB fault is [engine.Retryable]; a missing or out-of-scope
// connector is terminal (and reported as not-found, not leaking cross-scope
// existence).
func (a *HTTPActivity) resolveConnector(ctx context.Context, rc *engine.RunContext, ref string) (db.Connector, error) {
	id, err := parseConnectorLeaf(ref)
	if err != nil {
		return db.Connector{}, fmt.Errorf("connector: reference %q is not a valid connector resource name: %w", ref, err)
	}
	conn, err := a.store.GetConnector(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Connector{}, fmt.Errorf("connector: %q not found", ref)
		}
		return db.Connector{}, engine.Retryable(fmt.Errorf("connector: resolve %q: %w", ref, err))
	}
	orgID, spaceID := rc.Scope()
	if conn.OrgID != orgID || !sameSpace(conn.SpaceID, spaceID) {
		// Not in the run's scope — treat as not found so a crafted cross-scope
		// reference can't confirm the connector exists.
		return db.Connector{}, fmt.Errorf("connector: %q not found", ref)
	}
	return conn, nil
}

// buildRequest evaluates the activity's path, query, headers, and body against
// the run-context env and assembles the broker [Request].
func (a *HTTPActivity) buildRequest(ctx context.Context, rc *engine.RunContext, spec *workflowsv1.HttpActivity) (Request, error) {
	method := strings.ToUpper(strings.TrimSpace(spec.GetMethod()))
	if method == "" {
		return Request{}, fmt.Errorf("connector: http activity has no method")
	}

	var path string
	if expr := spec.GetPath(); expr != "" {
		var err error
		if path, err = a.eval.EvalString(ctx, expr, rc); err != nil {
			return Request{}, fmt.Errorf("connector: path: %w", err)
		}
	}

	query, err := a.evalStringMap(ctx, rc, spec.GetQuery(), "query")
	if err != nil {
		return Request{}, err
	}
	headers, err := a.evalStringMap(ctx, rc, spec.GetHeaders(), "header")
	if err != nil {
		return Request{}, err
	}

	var body []byte
	if expr := spec.GetBody(); expr != "" {
		val, err := a.eval.EvalAny(ctx, expr, rc)
		if err != nil {
			return Request{}, fmt.Errorf("connector: body: %w", err)
		}
		if body, err = json.Marshal(val); err != nil {
			return Request{}, fmt.Errorf("connector: marshal body: %w", err)
		}
	}

	return Request{
		Method:  method,
		Path:    path,
		Query:   query,
		Headers: headers,
		Body:    body,
	}, nil
}

// evalStringMap evaluates each CEL value in m against the run-context env,
// requiring a string result. kind names the field for error context.
func (a *HTTPActivity) evalStringMap(ctx context.Context, rc *engine.RunContext, m map[string]string, kind string) (map[string]string, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(m))
	for name, expr := range m {
		v, err := a.eval.EvalString(ctx, expr, rc)
		if err != nil {
			return nil, fmt.Errorf("connector: %s %q: %w", kind, name, err)
		}
		out[name] = v
	}
	return out, nil
}

// isTransient reports whether err is an in-process-retryable HTTP outcome. It is
// the retry classifier passed to [engine.WithRetry]; it deliberately does NOT
// match [engine.RetryableError] (an infra fault that must escalate to a job
// retry, not loop in-process) nor terminal errors.
func isTransient(err error) bool {
	var te *transientError
	return errors.As(err, &te)
}

// retryPolicy maps a proto RetryPolicy to the engine policy. A nil policy means a
// single attempt (no retry), per the field's documented default; unset sub-fields
// fall back to the engine defaults inside WithRetry.
func retryPolicy(p *workflowsv1.RetryPolicy) engine.RetryPolicy {
	if p == nil {
		return engine.RetryPolicy{MaxAttempts: 1}
	}
	return engine.RetryPolicy{
		MaxAttempts:    int(p.GetMaxAttempts()),
		InitialBackoff: p.GetInitialBackoff().AsDuration(),
		MaxBackoff:     p.GetMaxBackoff().AsDuration(),
		Multiplier:     p.GetBackoffMultiplier(),
	}
}

// isSuccess reports whether status is a success: any 2xx, or a code explicitly
// listed in success_status.
func isSuccess(status int, extra []int32) bool {
	if status >= 200 && status < 300 {
		return true
	}
	return containsStatus(extra, status)
}

// isRetryableStatus reports whether a non-success status is retryable: any 5xx,
// or a code explicitly listed in retryable_status. success_status is checked
// first by the caller, so a code in both is treated as success.
func isRetryableStatus(status int, extra []int32) bool {
	if status >= 500 && status <= 599 {
		return true
	}
	return containsStatus(extra, status)
}

func containsStatus(list []int32, status int) bool {
	for _, c := range list {
		if int(c) == status {
			return true
		}
	}
	return false
}

// httpOutput shapes a successful response as the step output exposed at
// steps.<id>.output. body is the raw response text; status is an int; headers
// map each name to its comma-joined values.
func httpOutput(resp *Response) map[string]any {
	return map[string]any{
		"status":  int64(resp.Status),
		"headers": headerMap(resp.Headers),
		"body":    string(resp.Body),
	}
}

func headerMap(h http.Header) map[string]any {
	out := make(map[string]any, len(h))
	for k, v := range h {
		out[k] = strings.Join(v, ", ")
	}
	return out
}

// parseConnectorLeaf extracts the leaf UUID from a Connector resource name
// ("organizations/{org}[/spaces/{space}]/connectors/{uuid}").
func parseConnectorLeaf(name string) (uuid.UUID, error) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return uuid.Nil, errors.New("not a connector resource name")
	}
	id, err := uuid.Parse(name[idx+1:])
	if err != nil {
		return uuid.Nil, errors.New("connector name leaf is not a uuid")
	}
	return id, nil
}

// sameSpace reports whether a connector's (nullable) space matches the run's
// space. uuid.Nil run space means org-scoped, which matches only a connector
// with no space.
func sameSpace(connSpace pgtype.UUID, runSpace uuid.UUID) bool {
	if runSpace == uuid.Nil {
		return !connSpace.Valid
	}
	return connSpace.Valid && uuid.UUID(connSpace.Bytes) == runSpace
}
