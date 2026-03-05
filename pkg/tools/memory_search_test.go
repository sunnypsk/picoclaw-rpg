package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/memorysearch"
)

func TestMemorySearchTool_Execute(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "memory-search-tool-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(tmpDir, "MEMORY.md"),
		[]byte("user_timezone: Asia/Hong_Kong\npreferred_language: zh-HK"),
		0o644,
	); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	tool := NewMemorySearchTool(memorysearch.NewIndex(tmpDir))
	result := tool.Execute(context.Background(), map[string]any{"query": "timezone", "limit": 3})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	var payload struct {
		Query   string           `json:"query"`
		Count   int              `json:"count"`
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("decode payload: %v\nraw: %s", err, result.ForLLM)
	}
	if payload.Query != "timezone" {
		t.Fatalf("query = %q, want %q", payload.Query, "timezone")
	}
	if payload.Count == 0 || len(payload.Results) == 0 {
		t.Fatalf("expected non-empty results, got count=%d", payload.Count)
	}
}
