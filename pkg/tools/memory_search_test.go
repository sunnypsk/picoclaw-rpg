package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/logger"
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

func TestMemorySearchTool_LogsResultPreview(t *testing.T) {
	tmpDir := t.TempDir()

	initialLevel := logger.GetLevel()
	defer logger.SetLevel(initialLevel)

	logPath := filepath.Join(tmpDir, "memory-search-tool.log")
	if err := logger.EnableFileLogging(logPath); err != nil {
		t.Fatalf("EnableFileLogging() error: %v", err)
	}
	defer logger.DisableFileLogging()

	logger.SetLevel(logger.INFO)

	if err := os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(tmpDir, "MEMORY.md"),
		[]byte("user_name: Sunny\npreferred_language: zh-HK"),
		0o644,
	); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	tool := NewMemorySearchTool(memorysearch.NewIndex(tmpDir))
	result := tool.Execute(context.Background(), map[string]any{"query": "name", "limit": 3})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	logText := string(raw)

	if !strings.Contains(logText, `"message":"memory_search result preview"`) {
		t.Fatalf("expected tool preview log, got: %s", logText)
	}
	if !strings.Contains(logText, `"top_result_path":"MEMORY.md"`) {
		t.Fatalf("expected top result path in logs, got: %s", logText)
	}
	if !strings.Contains(logText, `"top_result_snippet_preview"`) {
		t.Fatalf("expected top result snippet preview in logs, got: %s", logText)
	}
}
