package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dashkan/pivox/tools/internal/pivoxgen"
)

// TestEmitBridge_AiChat drives EmitBridge against the canned AiChat
// descriptor fixture and asserts the output matches the checked-in
// golden file. Regenerate the golden by running `go test ./... -update`.
func TestEmitBridge_AiChat(t *testing.T) {
	plugin := pivoxgen.LoadTestPlugin(t, "swift_module=PivoxModels",
		"pivox/ai/v1/ai_chat.proto")

	var f = plugin.Files[len(plugin.Files)-1]
	if f.Desc.Path() != "pivox/ai/v1/ai_chat.proto" {
		for _, x := range plugin.Files {
			if x.Desc.Path() == "pivox/ai/v1/ai_chat.proto" {
				f = x
				break
			}
		}
	}

	name, content := EmitBridge(f)
	if name != "pivox_ai_v1_ai_chat.bridge.swift" {
		t.Errorf("filename: got %q, want pivox_ai_v1_ai_chat.bridge.swift", name)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	goldenPath := filepath.Join(filepath.Dir(thisFile), "testdata",
		"ai_chat.bridge.swift.golden")

	if *pivoxgen.UpdateGoldenFlag {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(want) != content {
		t.Errorf("output mismatch. run `go test ./... -update` if intentional.\n\ngot:\n%s\n\nwant:\n%s",
			content, string(want))
	}
}
