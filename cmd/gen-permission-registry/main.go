// Permission-registry generator. Walks every gRPC method
// descriptor registered in the binary's proto registry, reads the
// `pivox.permission.v1` method options (required_permission /
// exempt / scope_field), and emits
// internal/server/permission_registry_gen.go containing the merged
// Registry and exempt set consumed by server.PermissionInterceptor.
//
// Adding gating to a new RPC:
//
//  1. Annotate the RPC in its .proto:
//     option (pivox.permission.v1.required_permission) = "x.y";
//     // and optionally:
//     option (pivox.permission.v1.scope_field) = "resource.name";
//     Or mark it exempt:
//     option (pivox.permission.v1.exempt) = true;
//  2. Run `make generate`.
//  3. Compile errors guide you to the rest. Drift-guards in tests
//     catch any RPC that has neither annotation.
//
// Scope kind (org vs space) is derived from the slug at the
// runtime path itself via server.ScopeFromPath, so there's no
// per-RPC scope_kind annotation.
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Blank-imports for every gRPC package we want to scan. The
	// generator's job is to pull annotations off these descriptors,
	// so they need to be loaded into protoregistry.GlobalFiles.
	_ "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	_ "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	_ "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	_ "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	_ "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	_ "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"

	permissionv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/permission/v1"
)

// outputPath is relative to the package directory containing the
// //go:generate directive (internal/server/), which is the CWD
// `go generate` sets when invoking us. When running the generator
// directly from repo root, paths into internal/server/ have to be
// supplied explicitly.
const outputPath = "permission_registry_gen.go"

// gatedServices is the set of fully-qualified service names whose
// methods are walked for permission annotations. Services not in
// this set are skipped (e.g. third-party services like
// google.longrunning.Operations).
var gatedServices = map[string]bool{
	"pivox.api.v1.Organizations":       true,
	"pivox.api.v1.Spaces":              true,
	"pivox.api.v1.TagKeys":             true,
	"pivox.api.v1.TagValues":           true,
	"pivox.api.v1.TagBindings":         true,
	"pivox.api.v1.ApiKeys":             true,
	"pivox.iam.v1.Iam":                 true,
	"pivox.assets.v1.Assets":           true,
	"pivox.assets.v1.Requests":         true,
	"pivox.storage.v1.StorageGateways": true,
	"pivox.storage.v1.Agents":          true,
	"pivox.storage.v1.Endpoints":       true,
	"pivox.ai.v1.AiChat":               true,
}

// goPkgInfo maps a proto package name (e.g. "pivox.iam.v1") to the
// Go import alias and import path used in the generated file. The
// alias is keyed by where a *message* is defined (NOT by the
// service that uses it) — Member RPCs on Organizations and Spaces
// reuse iamv1.GetMemberRequest etc., so the package alias must
// follow the request type's parent file.
var goPkgInfo = map[string]struct {
	alias string
	path  string
}{
	"pivox.api.v1":     {"apiv1", "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"},
	"pivox.iam.v1":     {"iamv1", "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"},
	"pivox.assets.v1":  {"assetsv1", "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"},
	"pivox.storage.v1": {"storagev1", "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"},
	"pivox.ai.v1":      {"aiv1", "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"},
}

type entry struct {
	FullMethod string // /pkg.Service/Method
	Permission string // permission ID (empty if exempt)
	Exempt     bool
	InputType  string // Go type name without package prefix, e.g. "GetOrganizationRequest"
	PkgAlias   string
	ScopeField string // dotted path; empty means default
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-permission-registry:", err)
		os.Exit(1)
	}
}

func run() error {
	var entries []entry
	var unannotated []string
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			if !gatedServices[string(svc.FullName())] {
				// Service we don't gate at all (e.g. third-party).
				continue
			}
			methods := svc.Methods()
			for j := 0; j < methods.Len(); j++ {
				m := methods.Get(j)
				e, annotated := buildEntry(m, svc)
				if !annotated {
					unannotated = append(unannotated, fmt.Sprintf("/%s/%s", svc.FullName(), m.Name()))
					continue
				}
				entries = append(entries, e)
			}
		}
		return true
	})

	if len(unannotated) > 0 {
		sort.Strings(unannotated)
		return fmt.Errorf("the following gated-service RPCs have neither required_permission nor exempt set:\n  %s", strings.Join(unannotated, "\n  "))
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].FullMethod < entries[j].FullMethod })

	out, err := emit(entries)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}

func buildEntry(m protoreflect.MethodDescriptor, svc protoreflect.ServiceDescriptor) (entry, bool) {
	opts := m.Options()
	if opts == nil {
		return entry{}, false
	}
	perm, _ := proto.GetExtension(opts, permissionv1.E_RequiredPermission).(string)
	exempt, _ := proto.GetExtension(opts, permissionv1.E_Exempt).(bool)
	scopeField, _ := proto.GetExtension(opts, permissionv1.E_ScopeField).(string)
	if perm == "" && !exempt {
		return entry{}, false
	}
	if perm != "" && exempt {
		// Both set — flag at generation time.
		return entry{
			FullMethod: fmt.Sprintf("/%s/%s", svc.FullName(), m.Name()),
			Permission: "<<both required_permission and exempt set — proto error>>",
		}, true
	}
	// Determine package alias from where the request type is
	// DEFINED, not from the service that uses it. Cross-package
	// reuse (e.g. iamv1.GetMemberRequest used on Organizations
	// and Spaces services) requires this lookup.
	inputPkg := string(m.Input().ParentFile().Package())
	info := goPkgInfo[inputPkg]
	return entry{
		FullMethod: fmt.Sprintf("/%s/%s", svc.FullName(), m.Name()),
		Permission: perm,
		Exempt:     exempt,
		InputType:  string(m.Input().Name()),
		PkgAlias:   info.alias,
		ScopeField: scopeField,
	}, true
}

// fieldGetter translates a proto field path ("name", "parent",
// "organization.name", "member.name", "sso_config.name") into the
// chain of Go accessor calls (".GetName()",
// ".GetOrganization().GetName()", etc.). Each dot-separated segment
// becomes ".Get<UpperCamelCase>()".
func fieldGetter(path string) string {
	if path == "" {
		path = "name"
	}
	var b strings.Builder
	for _, seg := range strings.Split(path, ".") {
		b.WriteString(".Get")
		b.WriteString(toCamel(seg))
		b.WriteString("()")
	}
	return b.String()
}

func toCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// inferScopeFieldDefault picks "parent" if the request has a parent
// field but no name field; otherwise "name". Mirrors AIP conventions
// (Create/List use parent, Get/Update/Delete use name).
func inferScopeFieldDefault(input protoreflect.MessageDescriptor) string {
	hasName := input.Fields().ByName("name") != nil
	hasParent := input.Fields().ByName("parent") != nil
	switch {
	case !hasName && hasParent:
		return "parent"
	case hasName && !hasParent:
		return "name"
	case hasName && hasParent:
		// Ambiguous — caller must set scope_field explicitly.
		return ""
	default:
		return ""
	}
}

func emit(entries []entry) ([]byte, error) {
	// Determine field default for each entry by re-walking the
	// registry. Doing this in emit (not buildEntry) keeps buildEntry
	// pure on the descriptor inputs.
	for i := range entries {
		if entries[i].Exempt {
			continue
		}
		if entries[i].ScopeField == "" {
			entries[i].ScopeField = lookupDefaultScopeField(entries[i].FullMethod)
			if entries[i].ScopeField == "" {
				return nil, fmt.Errorf("method %s: cannot infer scope_field (request has both 'name' and 'parent', or neither); set option (pivox.permission.v1.scope_field)", entries[i].FullMethod)
			}
		}
	}

	// Collect the unique set of (alias → import path) pairs used by
	// the entries. Each entry's PkgAlias was derived from the
	// request type's parent file, so the import block reflects the
	// real cross-package reuse (e.g. an Organizations service entry
	// importing iamv1 because GetMemberRequest lives there).
	pkgAliases := map[string]string{}
	for _, e := range entries {
		if e.Exempt || e.PkgAlias == "" {
			continue
		}
		for _, info := range goPkgInfo {
			if info.alias == e.PkgAlias {
				pkgAliases[info.alias] = info.path
				break
			}
		}
	}

	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/gen-permission-registry. DO NOT EDIT.\n")
	b.WriteString("// Source: pivox.permission.v1 method options on every gRPC method.\n\n")
	b.WriteString("package server\n\n")
	b.WriteString("import (\n")

	importPaths := []string{"github.com/dashkan/pivox/internal/permission"}
	aliasOrder := make([]string, 0, len(pkgAliases))
	for a := range pkgAliases {
		aliasOrder = append(aliasOrder, a)
	}
	sort.Strings(aliasOrder)
	for _, a := range aliasOrder {
		importPaths = append(importPaths, fmt.Sprintf("%s %q", a, pkgAliases[a]))
	}
	for i, ip := range importPaths {
		if i == 0 {
			fmt.Fprintf(&b, "\t%q\n\n", ip)
			continue
		}
		fmt.Fprintf(&b, "\t%s\n", ip)
	}
	b.WriteString(")\n\n")

	b.WriteString("// GeneratedRegistry is the union permission registry for every\n")
	b.WriteString("// gated gRPC method, derived from pivox.permission.v1 method\n")
	b.WriteString("// options. Pass to PermissionInterceptor along with\n")
	b.WriteString("// GeneratedExempt.\n")
	b.WriteString("var GeneratedRegistry = Registry{\n")
	for _, e := range entries {
		if e.Exempt {
			continue
		}
		fmt.Fprintf(&b, "\t%q: {\n", e.FullMethod)
		fmt.Fprintf(&b, "\t\tPermission: %s,\n", permissionConst(e.Permission))
		fmt.Fprintf(&b, "\t\tExtract: func(req any) (ScopeRef, error) {\n")
		fmt.Fprintf(&b, "\t\t\treturn ScopeFromPath(%q, req.(*%s.%s)%s)\n",
			e.ScopeField, e.PkgAlias, e.InputType, fieldGetter(e.ScopeField))
		fmt.Fprintf(&b, "\t\t},\n")
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n\n")

	b.WriteString("// GeneratedExempt is the union exempt set for every gRPC method\n")
	b.WriteString("// annotated `option (pivox.permission.v1.exempt) = true;`.\n")
	b.WriteString("// Bootstrap RPCs (CreateOrganization etc.) and circular ones\n")
	b.WriteString("// (TestIamPermissions) live here.\n")
	b.WriteString("var GeneratedExempt = map[string]bool{\n")
	for _, e := range entries {
		if !e.Exempt {
			continue
		}
		fmt.Fprintf(&b, "\t%q: true,\n", e.FullMethod)
	}
	b.WriteString("}\n")

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated Go: %w\n---\n%s", err, b.String())
	}
	return formatted, nil
}

// permissionConst converts a dotted permission ID like
// "organizations.update" into the Go constant name
// "permission.OrganizationsUpdate" emitted by cmd/gen-permissions.
func permissionConst(id string) string {
	parts := strings.Split(id, ".")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return "permission." + strings.Join(parts, "")
}

func svcOfMethod(fullMethod string) string {
	// /pkg.svc.X/Method → pkg.svc.X
	s := strings.TrimPrefix(fullMethod, "/")
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		return s[:idx]
	}
	return s
}

// lookupDefaultScopeField re-walks the registry to find the input
// type for fullMethod and infers a default scope field. Returns
// empty string if both name+parent (or neither) exist on the
// request.
func lookupDefaultScopeField(fullMethod string) string {
	svcName := svcOfMethod(fullMethod)
	methodName := fullMethod[strings.LastIndex(fullMethod, "/")+1:]
	var found string
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			if string(svc.FullName()) != svcName {
				continue
			}
			methods := svc.Methods()
			for j := 0; j < methods.Len(); j++ {
				m := methods.Get(j)
				if string(m.Name()) != methodName {
					continue
				}
				found = inferScopeFieldDefault(m.Input())
				return false
			}
		}
		return true
	})
	return found
}
