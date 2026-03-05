package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/memorysearch"
)

type MemorySearchTool struct {
	index *memorysearch.Index
}

func NewMemorySearchTool(index *memorysearch.Index) *MemorySearchTool {
	return &MemorySearchTool{index: index}
}

func (t *MemorySearchTool) Name() string {
	return "memory_search"
}

func (t *MemorySearchTool) Description() string {
	return "Search MEMORY.md and memory/*.md notes using local SQLite FTS5 keyword search"
}

func (t *MemorySearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results (default 5)",
			},
			"path_prefix": map[string]any{
				"type":        "string",
				"description": "Optional relative path prefix filter (for example memory/202603/)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *MemorySearchTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if t.index == nil {
		return ErrorResult("memory_search is unavailable: index is not initialized")
	}

	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return ErrorResult("query is required")
	}

	limit := 5
	if rawLimit, ok := args["limit"]; ok {
		switch v := rawLimit.(type) {
		case int:
			limit = v
		case int32:
			limit = int(v)
		case int64:
			limit = int(v)
		case float64:
			limit = int(v)
		}
	}
	if limit <= 0 {
		limit = 5
	}

	pathPrefix := ""
	if rawPrefix, ok := args["path_prefix"]; ok {
		if v, ok := rawPrefix.(string); ok {
			pathPrefix = strings.TrimSpace(v)
		}
	}

	results, err := t.index.Search(ctx, query, limit, pathPrefix)
	if err != nil {
		return ErrorResult(fmt.Sprintf("memory search failed: %v", err))
	}

	payload, err := json.Marshal(map[string]any{
		"query":   query,
		"count":   len(results),
		"results": results,
	})
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to encode memory search results: %v", err))
	}

	return NewToolResult(string(payload))
}
