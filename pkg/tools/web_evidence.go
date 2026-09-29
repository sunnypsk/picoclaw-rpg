package tools

import (
	"context"
	"encoding/json"
	"sync"
)

type evidenceKey struct{}
type WebEvidence struct {
	mu   sync.Mutex
	urls map[string]string
}

func WithWebEvidence(ctx context.Context) (context.Context, *WebEvidence) {
	e := &WebEvidence{urls: map[string]string{}}
	return context.WithValue(ctx, evidenceKey{}, e), e
}
func (e *WebEvidence) VerifiedURL(url string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	final, ok := e.urls[url]
	return final, ok
}
func recordWebEvidence(ctx context.Context, requested, final string) {
	if e, ok := ctx.Value(evidenceKey{}).(*WebEvidence); ok {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.urls[requested] = final
		e.urls[final] = final
	}
}

func webFetchFailure(kind string, status int, message string) *ToolResult {
	if len(message) > 1024 {
		message = message[:1024]
	}
	data, _ := json.Marshal(map[string]any{"fetch_state": kind, "status": status, "text": message})
	return &ToolResult{ForLLM: string(data), IsError: true}
}
