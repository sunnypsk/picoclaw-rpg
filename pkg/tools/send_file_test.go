package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/media"
)

func TestSendFileTool_Execute_Success(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "report.pdf")
	if err := os.WriteFile(filePath, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewSendFileTool(workspace, true)
	store := media.NewFileMediaStore()
	tool.SetMediaStore(store)
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })

	var sent bus.OutboundMediaMessage
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundMediaMessage) error {
		sent = msg
		return nil
	})

	ctx := WithToolContext(context.Background(), "whatsapp_native", "123456789@s.whatsapp.net")
	result := tool.Execute(ctx, map[string]any{
		"path":    "report.pdf",
		"caption": "monthly report",
	})

	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}
	if !result.Silent {
		t.Fatal("expected silent result")
	}
	if len(sent.Parts) != 1 {
		t.Fatalf("expected one outbound part, got %d", len(sent.Parts))
	}
	if sent.Channel != "whatsapp_native" || sent.ChatID != "123456789@s.whatsapp.net" {
		t.Fatalf("unexpected routing: %#v", sent)
	}
	part := sent.Parts[0]
	if part.Type != "file" {
		t.Fatalf("part.Type = %q, want file", part.Type)
	}
	if part.Filename != "report.pdf" {
		t.Fatalf("part.Filename = %q, want report.pdf", part.Filename)
	}
	if part.Caption != "monthly report" {
		t.Fatalf("part.Caption = %q, want monthly report", part.Caption)
	}
	if part.ContentType != "application/pdf" {
		t.Fatalf("part.ContentType = %q, want application/pdf", part.ContentType)
	}
	if !strings.HasPrefix(part.Ref, "media://") {
		t.Fatalf("part.Ref = %q, want media ref", part.Ref)
	}

	resolvedPath, meta, err := store.ResolveWithMeta(part.Ref)
	if err != nil {
		t.Fatalf("ResolveWithMeta() error = %v", err)
	}
	if resolvedPath != filePath {
		t.Fatalf("resolved path = %q, want %q", resolvedPath, filePath)
	}
	if meta.Source != "tool:send_file" || meta.Owned {
		t.Fatalf("unexpected media meta: %#v", meta)
	}
}

func TestSendFileTool_Execute_StickerSuccess(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "sticker.webp")
	if err := os.WriteFile(filePath, []byte("RIFFfakeWEBP"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewSendFileTool(workspace, true)
	store := media.NewFileMediaStore()
	tool.SetMediaStore(store)
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })

	var sent bus.OutboundMediaMessage
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundMediaMessage) error {
		sent = msg
		return nil
	})

	ctx := WithToolContext(context.Background(), "whatsapp_native", "123456789@s.whatsapp.net")
	result := tool.Execute(ctx, map[string]any{
		"path":       "sticker.webp",
		"as_sticker": true,
	})

	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}
	if !result.Silent {
		t.Fatal("expected silent result")
	}
	if len(sent.Parts) != 1 {
		t.Fatalf("expected one outbound part, got %d", len(sent.Parts))
	}
	part := sent.Parts[0]
	if part.Type != "sticker" {
		t.Fatalf("part.Type = %q, want sticker", part.Type)
	}
	if part.Caption != "" {
		t.Fatalf("part.Caption = %q, want empty", part.Caption)
	}
	if part.ContentType != "image/webp" {
		t.Fatalf("part.ContentType = %q, want image/webp", part.ContentType)
	}
}

func TestSendFileTool_Execute_RejectsPathOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.pdf")
	if err := os.WriteFile(outside, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewSendFileTool(workspace, true)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundMediaMessage) error { return nil })

	result := tool.Execute(WithToolContext(context.Background(), "whatsapp_native", "chat-1"), map[string]any{
		"path": outside,
	})
	if !result.IsError {
		t.Fatal("expected outside-workspace path to be rejected")
	}
	if !strings.Contains(result.ForLLM, "outside the workspace") {
		t.Fatalf("unexpected error: %q", result.ForLLM)
	}
}

func TestSendFileTool_Execute_RejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	secret := filepath.Join(root, "secret.pdf")
	if err := os.WriteFile(secret, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(workspace, "leak.pdf")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	tool := NewSendFileTool(workspace, true)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundMediaMessage) error { return nil })

	result := tool.Execute(WithToolContext(context.Background(), "whatsapp_native", "chat-1"), map[string]any{
		"path": link,
	})
	if !result.IsError {
		t.Fatal("expected symlink escape to be rejected")
	}
	if !strings.Contains(result.ForLLM, "symlink resolves outside workspace") {
		t.Fatalf("unexpected error: %q", result.ForLLM)
	}
}

func TestSendFileTool_Execute_RejectsStickerCaption(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "sticker.webp")
	if err := os.WriteFile(filePath, []byte("RIFFfakeWEBP"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewSendFileTool(workspace, true)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundMediaMessage) error { return nil })

	result := tool.Execute(WithToolContext(context.Background(), "whatsapp_native", "chat-1"), map[string]any{
		"path":       "sticker.webp",
		"as_sticker": true,
		"caption":    "hello",
	})
	if !result.IsError {
		t.Fatal("expected sticker caption to be rejected")
	}
	if result.ForLLM != "stickers do not support captions" {
		t.Fatalf("unexpected error: %q", result.ForLLM)
	}
}

func TestSendFileTool_Execute_RejectsStickerNonWebP(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "sticker.png")
	if err := os.WriteFile(filePath, []byte("not-a-real-png"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewSendFileTool(workspace, true)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundMediaMessage) error { return nil })

	result := tool.Execute(WithToolContext(context.Background(), "whatsapp_native", "chat-1"), map[string]any{
		"path":       "sticker.png",
		"as_sticker": true,
	})
	if !result.IsError {
		t.Fatal("expected non-WebP sticker to be rejected")
	}
	if result.ForLLM != "stickers must be WebP files with content type image/webp" {
		t.Fatalf("unexpected error: %q", result.ForLLM)
	}
}

func TestSendFileTool_Execute_UnsupportedChannel(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "report.pdf")
	if err := os.WriteFile(filePath, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewSendFileTool(workspace, true)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })

	result := tool.Execute(WithToolContext(context.Background(), "telegram", "chat-1"), map[string]any{
		"path": "report.pdf",
	})
	if !result.IsError {
		t.Fatal("expected unsupported channel error")
	}
	if result.ForLLM != `file sending is not supported on channel "telegram"` {
		t.Fatalf("unexpected error: %q", result.ForLLM)
	}
}

func TestSendFileTool_Execute_StickerUnsupportedChannel(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "sticker.webp")
	if err := os.WriteFile(filePath, []byte("RIFFfakeWEBP"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewSendFileTool(workspace, true)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.SetSupportChecker(func(channel string) bool { return true })
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundMediaMessage) error { return nil })

	result := tool.Execute(WithToolContext(context.Background(), "telegram", "chat-1"), map[string]any{
		"path":       "sticker.webp",
		"as_sticker": true,
	})
	if !result.IsError {
		t.Fatal("expected sticker send to reject non-native channel")
	}
	if result.ForLLM != `sending stickers is only supported on channel "whatsapp_native"` {
		t.Fatalf("unexpected error: %q", result.ForLLM)
	}
}

func TestSendFileTool_Execute_SendFailure(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "voice.ogg")
	if err := os.WriteFile(filePath, []byte("OggS"), 0o644); err != nil {
		t.Fatal(err)
	}

	sendErr := errors.New("network error")
	tool := NewSendFileTool(workspace, true)
	tool.SetMediaStore(media.NewFileMediaStore())
	tool.SetSupportChecker(func(channel string) bool { return channel == "whatsapp_native" })
	tool.SetSendCallback(func(ctx context.Context, msg bus.OutboundMediaMessage) error {
		return sendErr
	})

	result := tool.Execute(WithToolContext(context.Background(), "whatsapp_native", "chat-1"), map[string]any{
		"path": "voice.ogg",
	})
	if !result.IsError {
		t.Fatal("expected send error")
	}
	if result.Err != sendErr {
		t.Fatalf("expected Err to be sendErr, got %v", result.Err)
	}
	if result.ForLLM != "sending file: network error" {
		t.Fatalf("unexpected error: %q", result.ForLLM)
	}
}
