// Package pivoxgen holds shared helpers for the three protoc plugins
// that together emit the Swift↔C++ interop bridge (Swift proto
// extensions, C++ ChatClientCore + Apple bridge, Swift async facade).
//
// Each plugin lives in its own binary under tools/cmd/ and is retired
// independently as Swift-C++ interop matures upstream. Common concerns
// (option parsing, descriptor walks, naming helpers) belong here.
package pivoxgen

import (
	"flag"

	"google.golang.org/protobuf/compiler/protogen"
)

// CommonOptions captures flags shared across all three plugins.
type CommonOptions struct {
	CppNamespace  string
	CppClass      string
	SwiftModule   string
	ServiceFilter string // comma-separated service FullNames; empty = emit all
}

// RegisterCommonFlags wires the common flags into the given FlagSet.
// Individual plugins can register additional flags for their specific
// outputs.
func RegisterCommonFlags(fs *flag.FlagSet, opts *CommonOptions) {
	fs.StringVar(&opts.CppNamespace, "cpp_namespace", "pivox::ai_chat",
		"C++ namespace for the generated bridge")
	fs.StringVar(&opts.CppClass, "cpp_class", "ChatClient",
		"Apple Swift-bridge class name (inherits from <class>Core)")
	fs.StringVar(&opts.SwiftModule, "swift_module", "PivoxModels",
		"Swift module holding the proto types")
	fs.StringVar(&opts.ServiceFilter, "service_filter", "",
		"comma-separated service FullNames to emit for; empty = all")
}

// ShouldGenerateService reports whether the given service FullName
// passes the optional filter. Used by service-scoped plugins (cpp-
// bridge, swift-facade) to skip services outside the filter.
func ShouldGenerateService(opts *CommonOptions, fullName string) bool {
	if opts.ServiceFilter == "" {
		return true
	}
	for _, s := range splitCSV(opts.ServiceFilter) {
		if s == fullName {
			return true
		}
	}
	return false
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			if start < i {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// Proto3OptionalFeature declares to buf/protoc that the plugin
// understands proto3 `optional` fields. Required for current proto
// codegen; buf warns without it.
const Proto3OptionalFeature = 1

// SetSupportedFeatures is a small helper so each plugin's main() stays
// one-liner clean.
func SetSupportedFeatures(p *protogen.Plugin) {
	p.SupportedFeatures = Proto3OptionalFeature
}
