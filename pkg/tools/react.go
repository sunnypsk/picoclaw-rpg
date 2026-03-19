package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/bus"
)

type ReactionCallback func(ctx context.Context, msg bus.OutboundReactionMessage) error

type ReactTool struct {
	sendCallback   ReactionCallback
	supportChecker func(channel string) bool
}

func NewReactTool() *ReactTool {
	return &ReactTool{}
}

func (t *ReactTool) Name() string {
	return "react"
}

func (t *ReactTool) Description() string {
	return "Send an emoji reaction to the user's current message. Use sparingly for lightweight acknowledgement or tone."
}

func (t *ReactTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"emoji": map[string]any{
				"type":        "string",
				"description": "The emoji reaction to send to the current user message.",
			},
		},
		"required": []string{"emoji"},
	}
}

func (t *ReactTool) SetSendCallback(callback ReactionCallback) {
	t.sendCallback = callback
}

func (t *ReactTool) SetSupportChecker(checker func(channel string) bool) {
	t.supportChecker = checker
}

func (t *ReactTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	emoji, ok := args["emoji"].(string)
	if !ok {
		return &ToolResult{ForLLM: "emoji is required", IsError: true}
	}
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return &ToolResult{ForLLM: "emoji is required", IsError: true}
	}

	channel := ToolChannel(ctx)
	chatID := ToolChatID(ctx)
	messageID := ToolMessageID(ctx)
	senderID := ToolSenderID(ctx)
	if channel == "" || chatID == "" || messageID == "" || senderID == "" {
		return &ToolResult{ForLLM: "No target message context specified", IsError: true}
	}

	if t.supportChecker != nil && !t.supportChecker(channel) {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("reactions are not supported on channel %q", channel),
			IsError: true,
		}
	}
	if t.sendCallback == nil {
		return &ToolResult{ForLLM: "Reaction sending not configured", IsError: true}
	}

	msg := bus.OutboundReactionMessage{
		Channel:        channel,
		ChatID:         chatID,
		MessageID:      messageID,
		TargetSenderID: senderID,
		Emoji:          emoji,
	}
	if err := t.sendCallback(ctx, msg); err != nil {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("sending reaction: %v", err),
			IsError: true,
			Err:     err,
		}
	}

	return &ToolResult{
		ForLLM: fmt.Sprintf("Reaction %s sent to %s:%s on message %s", emoji, channel, chatID, messageID),
		Silent: true,
	}
}
