package tools

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/utils"
)

type SendMediaCallback func(ctx context.Context, msg bus.OutboundMediaMessage) error

type SendFileTool struct {
	workspace      string
	restrict       bool
	mediaStore     media.MediaStore
	sendCallback   SendMediaCallback
	supportChecker func(channel string) bool
}

func NewSendFileTool(workspace string, restrict bool) *SendFileTool {
	return &SendFileTool{
		workspace: workspace,
		restrict:  restrict,
	}
}

func (t *SendFileTool) Name() string {
	return "send_file"
}

func (t *SendFileTool) Description() string {
	return "Send one local workspace file to the current chat as native media, or as a WhatsApp sticker."
}

func (t *SendFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to a file inside the current workspace.",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional caption to include when the channel supports it.",
			},
			"as_sticker": map[string]any{
				"type":        "boolean",
				"description": "When true, send the file as a WhatsApp sticker in native mode. Requires WebP and forbids captions.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *SendFileTool) SetMediaStore(store media.MediaStore) {
	t.mediaStore = store
}

func (t *SendFileTool) SetSendCallback(callback SendMediaCallback) {
	t.sendCallback = callback
}

func (t *SendFileTool) SetSupportChecker(checker func(channel string) bool) {
	t.supportChecker = checker
}

func (t *SendFileTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	rawPath, ok := args["path"].(string)
	if !ok || strings.TrimSpace(rawPath) == "" {
		return ErrorResult("path is required")
	}
	caption, _ := args["caption"].(string)
	caption = strings.TrimSpace(caption)
	asSticker, _ := args["as_sticker"].(bool)

	channel := ToolChannel(ctx)
	chatID := ToolChatID(ctx)
	if channel == "" || chatID == "" {
		return ErrorResult("No target channel/chat specified")
	}
	if asSticker && channel != "whatsapp_native" {
		return ErrorResult(`sending stickers is only supported on channel "whatsapp_native"`)
	}
	if t.supportChecker != nil && !t.supportChecker(channel) {
		return ErrorResult(fmt.Sprintf("file sending is not supported on channel %q", channel))
	}
	if t.mediaStore == nil {
		return ErrorResult("File sending is not configured with a media store")
	}
	if t.sendCallback == nil {
		return ErrorResult("File sending is not configured")
	}

	localPath, err := validatePath(strings.TrimSpace(rawPath), t.workspace, t.restrict)
	if err != nil {
		return ErrorResult(err.Error())
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to stat file: %v", err))
	}
	if info.IsDir() {
		return ErrorResult("path must point to a file, not a directory")
	}

	filename := filepath.Base(localPath)
	contentType, err := detectLocalContentType(localPath)
	if err != nil {
		return ErrorResult(err.Error())
	}
	partType := utils.InferMediaType(filename, contentType)
	if asSticker {
		if caption != "" {
			return ErrorResult("stickers do not support captions")
		}
		if !isWhatsAppStickerContentType(contentType) {
			return ErrorResult("stickers must be WebP files with content type image/webp")
		}
		partType = "sticker"
	}
	scope := fmt.Sprintf("tool:send_file:%s:%s:%s", channel, chatID, uuid.NewString())
	ref, err := t.mediaStore.Store(localPath, media.MediaMeta{
		Filename:    filename,
		ContentType: contentType,
		Source:      "tool:send_file",
		Owned:       false,
	}, scope)
	if err != nil {
		return ErrorResult(fmt.Sprintf("store outbound file: %v", err)).WithError(err)
	}

	msg := bus.OutboundMediaMessage{
		Channel: channel,
		ChatID:  chatID,
		Parts: []bus.MediaPart{{
			Type:        partType,
			Ref:         ref,
			Caption:     caption,
			Filename:    filename,
			ContentType: contentType,
		}},
	}
	msg.ReplyToMessageID, msg.ReplyToSenderID = toolReplyTarget(ctx, channel, chatID)
	if err := t.sendCallback(ctx, msg); err != nil {
		return (&ToolResult{
			ForLLM:  fmt.Sprintf("sending file: %v", err),
			IsError: true,
			Err:     err,
		}).WithError(err)
	}

	resultKind := "File"
	if asSticker {
		resultKind = "Sticker"
	}
	return SilentResult(fmt.Sprintf("%s sent to %s:%s: %s", resultKind, channel, chatID, filename))
}

func detectLocalContentType(path string) (string, error) {
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType != "" && contentType != "application/octet-stream" {
		return contentType, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	header := make([]byte, 512)
	n, readErr := file.Read(header)
	if readErr != nil && readErr != io.EOF {
		return "", fmt.Errorf("failed to read file header: %w", readErr)
	}
	if n > 0 {
		detected := http.DetectContentType(header[:n])
		if detected != "" && detected != "application/octet-stream" {
			return detected, nil
		}
	}
	if contentType != "" {
		return contentType, nil
	}
	return "application/octet-stream", nil
}

func isWhatsAppStickerContentType(contentType string) bool {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return false
	}

	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = parsed
	}

	return strings.EqualFold(contentType, "image/webp")
}
