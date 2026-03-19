package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
)

func TestReactTool_Execute_Success(t *testing.T) {
	tool := NewReactTool()
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })

	var sent bus.OutboundReactionMessage
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundReactionMessage) error {
		sent = msg
		return nil
	})

	ctx := WithToolMessageContext(
		context.Background(),
		"whatsapp_native",
		"123456789@s.whatsapp.net",
		"wamid-1",
		"123456789@s.whatsapp.net",
	)
	result := tool.Execute(ctx, map[string]any{"emoji": "🙏"})

	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}
	if !result.Silent {
		t.Fatal("expected silent result")
	}
	if sent.Channel != "whatsapp_native" || sent.ChatID != "123456789@s.whatsapp.net" {
		t.Fatalf("unexpected routing: %#v", sent)
	}
	if sent.MessageID != "wamid-1" || sent.TargetSenderID != "123456789@s.whatsapp.net" {
		t.Fatalf("unexpected target context: %#v", sent)
	}
	if sent.Emoji != "🙏" {
		t.Fatalf("emoji = %q, want %q", sent.Emoji, "🙏")
	}
}

func TestReactTool_Execute_MissingEmoji(t *testing.T) {
	tool := NewReactTool()
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing emoji")
	}
	if result.ForLLM != "emoji is required" {
		t.Fatalf("unexpected error message: %q", result.ForLLM)
	}
}

func TestReactTool_Execute_MissingMessageContext(t *testing.T) {
	tool := NewReactTool()
	result := tool.Execute(WithToolContext(context.Background(), "whatsapp_native", "chat-1"), map[string]any{"emoji": "👍"})
	if !result.IsError {
		t.Fatal("expected error for missing message context")
	}
	if result.ForLLM != "No target message context specified" {
		t.Fatalf("unexpected error message: %q", result.ForLLM)
	}
}

func TestReactTool_Execute_UnsupportedChannel(t *testing.T) {
	tool := NewReactTool()
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })

	ctx := WithToolMessageContext(context.Background(), "telegram", "chat-1", "msg-1", "user-1")
	result := tool.Execute(ctx, map[string]any{"emoji": "👍"})
	if !result.IsError {
		t.Fatal("expected unsupported channel error")
	}
	if result.ForLLM != `reactions are not supported on channel "telegram"` {
		t.Fatalf("unexpected error message: %q", result.ForLLM)
	}
}

func TestReactTool_Execute_SendFailure(t *testing.T) {
	tool := NewReactTool()
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })

	sendErr := errors.New("network error")
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundReactionMessage) error {
		return sendErr
	})

	ctx := WithToolMessageContext(context.Background(), "whatsapp_native", "chat-1", "msg-1", "user-1")
	result := tool.Execute(ctx, map[string]any{"emoji": "👍"})
	if !result.IsError {
		t.Fatal("expected send error")
	}
	if result.Err != sendErr {
		t.Fatalf("expected Err to be sendErr, got %v", result.Err)
	}
	if result.ForLLM != "sending reaction: network error" {
		t.Fatalf("unexpected error message: %q", result.ForLLM)
	}
}
