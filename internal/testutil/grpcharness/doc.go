// Package grpcharness wires up an in-memory gRPC server with the
// production interceptor chain (Auth + Membership + Permission +
// Validate) against a real Postgres test container, plus seed
// helpers for identities, organizations, and member bindings. It is
// the canonical end-to-end test scaffold for any service that
// participates in the IAM model.
//
// # Why a shared package
//
// Each service's _integration_test.go used to spin up a plain
// gRPC server with no interceptors. Phase 4 lifecycle/member/domain
// handlers call MustResolvedOrgFromContext from the permission
// interceptor — they panic without it. Building a one-off
// interceptor pipeline in every service test invites drift between
// services and between test and production.
//
// One harness, used by every service-level integration test, keeps
// the chain canonical. When the production chain changes, the
// harness changes once and every service test picks it up.
//
// # Caller identity model
//
// The harness's test-mode authn.Service trusts the bearer token
// verbatim — token IS the UID. SetCaller swaps the outgoing
// metadata that subsequent gRPC calls carry. This avoids the
// per-call `metadata.AppendToOutgoingContext` boilerplate at every
// call site and matches the production semantic where the token
// uniquely identifies one Firebase user.
package grpcharness
