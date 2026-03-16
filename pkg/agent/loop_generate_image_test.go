package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/media"
)

func TestAgentLoopSetMediaStoreBindsGenerateImageTool(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	agent := al.registry.GetDefaultAgent()
	toolAny, ok := agent.Tools.Get("generate_image")
	if !ok {
		t.Fatal("expected generate_image tool to be registered")
	}

	result := toolAny.Execute(context.Background(), map[string]any{"prompt": "cat"})
	if !result.IsError || !strings.Contains(result.ForLLM, "media store") {
		t.Fatalf("expected missing media store error, got %#v", result)
	}

	al.SetMediaStore(media.NewFileMediaStore())

	toolAny, ok = agent.Tools.Get("generate_image")
	if !ok {
		t.Fatal("expected generate_image tool after media store injection")
	}
	result = toolAny.Execute(context.Background(), map[string]any{"prompt": "cat"})
	if !result.IsError || !strings.Contains(result.ForLLM, "CPA_API_KEY") {
		t.Fatalf("expected missing CPA env error after media store injection, got %#v", result)
	}
}
