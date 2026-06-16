package agent

import (
	"context"
	"testing"

	"github.com/sipeed/picoclaw/pkg/media"
)

func TestAgentLoopSetMediaStoreBindsGeneratePresentationTool(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	agent := al.registry.GetDefaultAgent()
	toolAny, ok := agent.Tools.Get("generate_presentation")
	if !ok {
		t.Fatal("expected generate_presentation tool to be registered")
	}

	args := map[string]any{
		"title": "Launch Brief",
		"slides": []any{
			map[string]any{
				"layout": "cover",
				"title":  "Launch Brief",
			},
		},
	}

	result := toolAny.Execute(context.Background(), args)
	if result.IsError {
		t.Fatalf("expected success without media store, got %#v", result)
	}
	if len(result.Media) != 0 {
		t.Fatalf("expected no media without media store, got %#v", result.Media)
	}

	al.SetMediaStore(media.NewFileMediaStore())

	toolAny, ok = agent.Tools.Get("generate_presentation")
	if !ok {
		t.Fatal("expected generate_presentation tool after media store injection")
	}
	result = toolAny.Execute(context.Background(), args)
	if result.IsError {
		t.Fatalf("expected success after media store injection, got %#v", result)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected ZIP media ref after media store injection, got %#v", result.Media)
	}
}
