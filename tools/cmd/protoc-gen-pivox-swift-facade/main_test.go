package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/dashkan/pivox/tools/internal/pivoxgen"
)

func TestEmitService_AiChat(t *testing.T) {
	plugin := pivoxgen.LoadTestPlugin(t,
		"cpp_namespace=pivox::ai_chat,cpp_class=ChatClient,swift_module=PivoxModels",
		"pivox/ai/v1/ai_chat.proto")

	svc := findService(t, plugin, "pivox.ai.v1.AiChat")

	if err := emitService(plugin, svc); err != nil {
		t.Fatalf("emitService: %v", err)
	}

	resp := plugin.Response()
	if len(resp.File) != 1 {
		t.Fatalf("expected 1 emitted file, got %d", len(resp.File))
	}
	got := resp.File[0].GetContent()

	_, thisFile, _, _ := runtime.Caller(0)
	goldenPath := filepath.Join(filepath.Dir(thisFile), "testdata",
		"AiChat+Facade.swift.golden")
	assertGolden(t, goldenPath, got)
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
