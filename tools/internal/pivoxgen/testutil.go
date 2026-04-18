package pivoxgen

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// UpdateGoldenFlag is a shared test flag — pass `-update` when running
// `go test` to rewrite golden files instead of comparing against them.
// Use after an intentional template change to refresh the expected
// output in bulk.
var UpdateGoldenFlag = flag.Bool("update", false, "rewrite golden test fixtures")

// LoadTestPlugin constructs a protogen.Plugin from the canned
// FileDescriptorSet fixture at `tools/testdata/ai_chat.descriptors.binpb`,
// with `filesToGenerate` (proto paths) marked for generation and the
// given plugin-option string. Shared by all three plugin test suites so
// fixture wrangling is centralised.
func LoadTestPlugin(t *testing.T, parameter string, filesToGenerate ...string) *protogen.Plugin {
	t.Helper()

	_, thisFile, _, _ := runtime.Caller(0)
	fixturePath := filepath.Join(filepath.Dir(thisFile), "..", "..",
		"testdata", "ai_chat.descriptors.binpb")

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("load fixture %s: %v", fixturePath, err)
	}

	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: filesToGenerate,
		Parameter:      &parameter,
		ProtoFile:      set.File,
	}

	// Mirror the param-handling main.go uses, but ignore unknown flags
	// so tests can reuse the same fixture regardless of which plugin's
	// options they care about.
	plugin, err := protogen.Options{
		ParamFunc: func(_, _ string) error { return nil },
	}.New(req)
	if err != nil {
		t.Fatalf("protogen.New: %v", err)
	}
	return plugin
}
