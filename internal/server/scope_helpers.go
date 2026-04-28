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
