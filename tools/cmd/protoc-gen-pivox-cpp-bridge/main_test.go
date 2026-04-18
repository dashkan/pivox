package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/dashkan/pivox/tools/internal/pivoxgen"
)

// TestEmitService_AiChat drives emitService against the canned AiChat
// descriptor fixture and asserts both generated files (.h.inc + .cc.inc)
// match their checked-in golden outputs. `go test ./... -update` to
// refresh goldens after an intentional template change.
func TestEmitService_AiChat(t *testing.T) {
	plugin := pivoxgen.LoadTestPlugin(t,
		"cpp_namespace=pivox::ai_chat,cpp_class=ChatClient,swift_module=PivoxModels",
		"pivox/ai/v1/ai_chat.proto")

	svc := findService(t, plugin, "pivox.ai.v1.AiChat")

	if err := emitService(plugin, svc); err != nil {
		t.Fatalf("emitService: %v", err)
	}

	resp := plugin.Response()
	files := make(map[string]string, len(resp.File))
	for _, f := range resp.File {
		files[f.GetName()] = f.GetContent()
	}

	goldens := []struct {
		genName  string
		filename string
	}{
		{"ai_chat_bridge.h.inc", "ai_chat_bridge.h.inc.golden"},
		{"ai_chat_bridge.cc.inc", "ai_chat_bridge.cc.inc.golden"},
	}

	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "testdata")

	for _, g := range goldens {
		got, ok := files[g.genName]
		if !ok {
			t.Errorf("plugin did not emit %s; got: %v", g.genName, keys(files))
			continue
		}
		assertGolden(t, filepath.Join(dir, g.filename), got)
	}
}

func findService(t *testing.T, plugin *protogen.Plugin, fullName string) *protogen.Service {
	t.Helper()
	for _, f := range plugin.Files {
		for _, s := range f.Services {
			if string(s.Desc.FullName()) == fullName {
				return s
			}
		}
	}
	t.Fatalf("service %q not found in fixture", fullName)
	return nil
}

func assertGolden(t *testing.T, goldenPath, got string) {
	t.Helper()
	if *pivoxgen.UpdateGoldenFlag {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", goldenPath, err)
	}
	if string(want) != got {
		t.Errorf("%s mismatch. run `go test ./... -update` if intentional.\n\ngot:\n%s\n\nwant:\n%s",
			filepath.Base(goldenPath), got, string(want))
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestNoForbiddenTokens guards the include-inside-class pattern: the
// generated `.h.inc` is `#include`d directly inside `class ChatClient
// { ... }`, and the `.cc.inc` is included at TU scope. Any access-
// specifier or `using namespace` in the generated output structurally
// corrupts the surrounding hand-written code. This test asserts the
// current generator doesn't emit such tokens — caught at test time, not
// at Xcode link time on some cursed future afternoon.
func TestNoForbiddenTokens(t *testing.T) {
	plugin := pivoxgen.LoadTestPlugin(t,
		"cpp_namespace=pivox::ai_chat,cpp_class=ChatClient,swift_module=PivoxModels",
		"pivox/ai/v1/ai_chat.proto")

	svc := findService(t, plugin, "pivox.ai.v1.AiChat")
	if err := emitService(plugin, svc); err != nil {
		t.Fatalf("emitService: %v", err)
	}

	forbidden := []string{
		"private:",
		"public:",
		"protected:",
		"using namespace",
	}

	for _, f := range plugin.Response().File {
		content := f.GetContent()
		for _, tok := range forbidden {
			if strings.Contains(content, tok) {
				t.Errorf("%s contains forbidden token %q — would corrupt surrounding TU/class scope",
					f.GetName(), tok)
			}
		}
	}
}
