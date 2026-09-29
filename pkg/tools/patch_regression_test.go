package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/cron"
	"github.com/sipeed/picoclaw/pkg/media"
)

func TestInvalidEditMakesNoProviderCalls(t *testing.T) {
	for _, model := range []string{"gpt-image-2", "chat-image"} {
		t.Run(model, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer server.Close()
			dir := t.TempDir()
			path := filepath.Join(dir, "photo.jpg")
			if err := os.WriteFile(path, []byte("PK\x03\x04animation/animation.json"), 0600); err != nil {
				t.Fatal(err)
			}
			tool := NewGenerateImageTool(dir, true)
			tool.SetMediaStore(media.NewFileMediaStore())
			tool.getenv = func(key string) string {
				switch key {
				case "CPA_API_KEY":
					return "fake"
				case "CPA_API_BASE":
					return server.URL
				case "CPA_IMAGE_MODEL":
					return model
				}
				return ""
			}
			result := tool.Execute(context.Background(), map[string]any{"prompt": "edit", "image": path})
			if !result.IsError || calls.Load() != 0 {
				t.Fatalf("invalid input reached provider: %+v, calls=%d", result, calls.Load())
			}
		})
	}
}

func TestWebFetchEvidenceRequiresSuccessfulBody(t *testing.T) {
	for _, status := range []int{200, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("short valid article"))
			}))
			defer server.Close()
			tool, _ := NewWebFetchTool(1000, 4096)
			ctx, evidence := WithWebEvidence(context.Background())
			result := tool.Execute(ctx, map[string]any{"url": server.URL})
			_, verified := evidence.VerifiedURL(server.URL)
			if result.IsError != (status != 200) || verified != (status == 200) {
				t.Fatalf("wrong status/evidence: %+v, %v", result, verified)
			}
		})
	}
	for _, body := range []string{"", "<html><title>Just a moment</title><div>verify you are human</div></html>"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		tool, _ := NewWebFetchTool(1000, 4096)
		if result := tool.Execute(context.Background(), map[string]any{"url": server.URL}); !result.IsError {
			t.Fatal("empty/challenge page accepted")
		}
		server.Close()
	}
}

func TestMessageSendStateIsRequestScoped(t *testing.T) {
	tool := NewMessageTool()
	tool.SetSendCallback(func(context.Context, bus.OutboundMessage) error { return nil })
	first := WithSendState(WithToolContext(context.Background(), "test", "chat"))
	second := WithSendState(WithToolContext(context.Background(), "test", "chat"))
	result := tool.Execute(first, map[string]any{"content": "one"})
	if result.IsError || !MessageSent(first) || MessageSent(second) {
		t.Fatal("send state crossed requests")
	}
}

func TestCronSourceSessionNeverUsesInternalExecutionSession(t *testing.T) {
	ctx := WithToolExecutionContext(context.Background(), "whatsapp_native", "123@g.us", "", "", "agent:wrong:scheduled-reminder:old", nil)
	if ToolSourceSession(ctx) != "" {
		t.Fatal("internal session used as source")
	}
	ctx = WithSourceSession(ctx, "agent:group:whatsapp_native:group:123@g.us")
	if ToolSourceSession(ctx) != "agent:group:whatsapp_native:group:123@g.us" {
		t.Fatal("source lost")
	}
	ctx = WithToolExecutionContext(ctx, "whatsapp_native", "123@g.us", "", "", "agent:group:scheduled-reminder:new", nil)
	if ToolSourceSession(ctx) != "agent:group:whatsapp_native:group:123@g.us" {
		t.Fatal("nested cron lost source")
	}
}

type failingScheduledExecutor struct{}

func (failingScheduledExecutor) ProcessScheduledReminder(context.Context, ScheduledReminderRequest) (string, error) {
	return "", errors.New("verification failed")
}

func TestExecuteJobPropagatesRealError(t *testing.T) {
	tool := &CronTool{executor: failingScheduledExecutor{}}
	job := &cron.CronJob{ID: "test", Payload: cron.CronPayload{Channel: "whatsapp_native", To: "123@g.us"}}
	if _, err := tool.ExecuteJobWithError(context.Background(), job); err == nil {
		t.Fatal("execution error hidden")
	}
	if result := tool.ExecuteJob(context.Background(), job); result != "Error: verification failed" {
		t.Fatalf("legacy wrapper changed: %s", result)
	}
}
