package server

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/apierr"
)

// OrgScopeFromPath extracts an org-scoped ScopeRef from a resource
// path of the form `organizations/{org}` or `organizations/{org}/...`.
// Used by per-RPC ScopeExtractors that pull `name` or `parent` from
// a request and hand off the path here.
//
// `field` is the proto field name (e.g. "name", "parent",
// "organization.name") so the InvalidArgument error attributes the
// violation to the right field for the caller.
//
// Returns InvalidArgument for empty paths, wrong collection prefix,
// or missing slug segment. Per-extractor wrappers are tiny because
// all the shape-checking lives here.
func OrgScopeFromPath(field, path string) (ScopeRef, error) {
	if path == "" {
		return ScopeRef{}, apierr.InvalidArgument(apierr.FieldViolation(field, "must not be empty"))
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "organizations" || parts[1] == "" {
		return ScopeRef{}, apierr.InvalidArgument(apierr.FieldViolation(field,
			fmt.Sprintf("expected organizations/{org}[/...] in %q", path)))
	}
	return OrgScope(parts[1]), nil
}

// ScopeFromPath auto-discriminates between org-scope and space-scope
// from the path's shape. Generated extractors call this single
// helper so the generator doesn't have to know per-RPC which scope
// kind applies — the runtime path tells us:
//
//   - `organizations/{org}/spaces/{space}[/...]` → ScopeSpace
//   - `organizations/{org}[/...]` (without /spaces/) → ScopeOrg
//
// Anything else surfaces as InvalidArgument with the same field-
// violation shape the per-kind helpers use.
func ScopeFromPath(field, path string) (ScopeRef, error) {
	if path == "" {
		return ScopeRef{}, apierr.InvalidArgument(apierr.FieldViolation(field, "must not be empty"))
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "organizations" || parts[1] == "" {
		return ScopeRef{}, apierr.InvalidArgument(apierr.FieldViolation(field,
			fmt.Sprintf("expected organizations/{org}[/...] or organizations/{org}/spaces/{space}[/...] in %q", path)))
	}
	if len(parts) >= 3 && parts[2] == "spaces" {
		if len(parts) < 4 || parts[3] == "" {
			return ScopeRef{}, apierr.InvalidArgument(apierr.FieldViolation(field,
				fmt.Sprintf("missing space slug in %q", path)))
		}
		return SpaceScope(parts[1], parts[3]), nil
	}
	return OrgScope(parts[1]), nil
}

// SpaceScopeFromPath extracts a space-scoped ScopeRef from a resource
// path of the form `organizations/{org}/spaces/{space}` or
// `organizations/{org}/spaces/{space}/...`. Both the parent org slug
// and the space slug are pulled out — the interceptor needs both to
// resolve through the slug-immutable lookup chain.
//
// Returns InvalidArgument for empty paths, wrong collection prefix
// at either level, or missing slug at either level.
func SpaceScopeFromPath(field, path string) (ScopeRef, error) {
	if path == "" {
		return ScopeRef{}, apierr.InvalidArgument(apierr.FieldViolation(field, "must not be empty"))
	}
	parts := strings.Split(path, "/")
	if len(parts) < 4 ||
		parts[0] != "organizations" || parts[1] == "" ||
		parts[2] != "spaces" || parts[3] == "" {
		return ScopeRef{}, apierr.InvalidArgument(apierr.FieldViolation(field,
			fmt.Sprintf("expected organizations/{org}/spaces/{space}[/...] in %q", path)))
	}
	return SpaceScope(parts[1], parts[3]), nil
}

// AssertRegistryCoversService verifies that every RPC declared on
// `desc` is accounted for by `registry` ∪ `exempt`. Intended for use
// in service-level unit tests so a newly-added proto RPC fails its
// suite until it's explicitly gated or exempted. Catches the
// "added an RPC, forgot to wire it, gets Internal in prod" class of
// bug at build time rather than runtime.
//
// Returns the full method names that are uncovered (empty slice on
// success). Tests should fail when this returns non-empty.
func AssertRegistryCoversService(desc *grpc.ServiceDesc, registry Registry, exempt map[string]bool) []string {
	var uncovered []string
	prefix := "/" + desc.ServiceName + "/"
	for _, m := range desc.Methods {
		full := prefix + m.MethodName
		if _, ok := registry[full]; ok {
			continue
		}
		if exempt[full] {
			continue
		}
		uncovered = append(uncovered, full)
	}
	for _, s := range desc.Streams {
		full := prefix + s.StreamName
		if _, ok := registry[full]; ok {
			continue
		}
		if exempt[full] {
			continue
		}
		uncovered = append(uncovered, full)
	}
	sort.Strings(uncovered)
	return uncovered
}
