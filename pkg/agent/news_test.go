package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type newsTestProvider struct{ calls int }

func (p *newsTestProvider) GetDefaultModel() string { return "mock" }
func (p *newsTestProvider) Chat(ctx context.Context, m []providers.Message, defs []providers.ToolDefinition, model string, opts map[string]any) (*providers.LLMResponse, error) {
	p.calls++
	if p.calls == 1 {
		return &providers.LLMResponse{ToolCalls: []providers.ToolCall{{ID: "send", Name: "message", Arguments: map[string]any{"content": "unverified intermediate"}}}}, nil
	}
	return &providers.LLMResponse{Content: `{"status":"no_news","text":"no news","source_urls":["https://unread.example/article"]}`}, nil
}

func TestNewsGateBuffersToolMessagesAndFailsOnce(t *testing.T) {
	al, _, messageBus := newScheduledReminderLoop(t, &newsTestProvider{})
	result, err := al.ProcessScheduledReminder(context.Background(), tools.ScheduledReminderRequest{JobID: "news", Channel: "telegram", ChatID: "chat1", Content: "news", MinVerifiedSources: 2})
	if err == nil || result != newsFailure {
		t.Fatalf("expected failed verification: %q %v", result, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, ok := messageBus.SubscribeOutbound(ctx)
	if !ok || msg.Content != newsFailure || msg.TraceID == "" {
		t.Fatalf("wrong final message: %+v", msg)
	}
	short, cancelShort := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShort()
	if msg, ok := messageBus.SubscribeOutbound(short); ok {
		t.Fatalf("extra news output: %+v", msg)
	}
}

func TestNewsIndependentSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("A short real article.")) }))
	defer server.Close()
	ctx, evidence := tools.WithWebEvidence(context.Background())
	fetch, _ := tools.NewWebFetchTool(1024, 4096)
	for _, path := range []string{"/one", "/two"} {
		if result := fetch.Execute(ctx, map[string]any{"url": server.URL + path}); result.IsError {
			t.Fatal(result.ForLLM)
		}
	}
	data, _ := json.Marshal(map[string]any{"status": "no_news", "text": "No important news", "source_urls": []string{server.URL + "/one", server.URL + "/two"}})
	if _, err := validateNews(string(data), 2, evidence); err == nil {
		t.Fatal("same site counted twice")
	}
	if _, err := validateNews(string(data), 1, evidence); err != nil {
		t.Fatal(err)
	}
	_, empty := tools.WithWebEvidence(context.Background())
	if _, err := validateNews(string(data), 1, empty); err == nil {
		t.Fatal("prior-run evidence accepted")
	}
}

func TestNewsTwoIndependentSitesAndRecovery(t *testing.T) {
	status := http.StatusServiceUnavailable
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("Verified current article."))
	}))
	defer proxy.Close()
	fetch, err := tools.NewWebFetchToolWithProxy(1024, proxy.URL, 4096, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, evidence := tools.WithWebEvidence(context.Background())
	sources := []string{"http://one.example.org/article", "http://two.example.net/article"}
	raw, _ := json.Marshal(map[string]any{"status": "news", "items": []any{map[string]any{"text": "Important news", "source_urls": sources}}})
	for _, source := range sources {
		fetch.Execute(ctx, map[string]any{"url": source})
	}
	if _, err := validateNews(string(raw), 2, evidence); err == nil {
		t.Fatal("failed fetch counted")
	}
	status = http.StatusOK
	for _, source := range sources {
		if result := fetch.Execute(ctx, map[string]any{"url": source}); result.IsError {
			t.Fatal(result.ForLLM)
		}
	}
	if _, err := validateNews(string(raw), 2, evidence); err != nil {
		t.Fatal(err)
	}
}

func TestScheduledReminderRejectsInternalDirectGroupSource(t *testing.T) {
	al, _, _ := newScheduledReminderLoop(t, &mockProvider{})
	req := tools.ScheduledReminderRequest{Channel: "whatsapp_native", ChatID: "123@g.us", SessionKey: "agent:main:scheduled-reminder:0832"}
	input, ok := scheduledReminderRouteInput(req, req.SessionKey)
	if !ok || input.Peer.Kind != "group" || input.Peer.ID != req.ChatID {
		t.Fatalf("wrong source: %+v", input)
	}
	_, session := al.resolveScheduledReminderTarget(req)
	if session == req.SessionKey {
		t.Fatal("retained internal session")
	}
}
