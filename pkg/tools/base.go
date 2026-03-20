package tools

import (
	"context"
	"strings"
)

// Tool is the interface that all tools must implement.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, args map[string]any) *ToolResult
}

// ContextualTool is an optional interface that tools can implement
// to receive the current message context (channel, chatID).
type ContextualTool interface {
	Tool
	SetContext(channel, chatID string)
}

type toolCtxKey struct{ name string }

var (
	ctxKeyChannel    = &toolCtxKey{"channel"}
	ctxKeyChatID     = &toolCtxKey{"chatID"}
	ctxKeyMessageID  = &toolCtxKey{"messageID"}
	ctxKeySenderID   = &toolCtxKey{"senderID"}
	ctxKeySessionKey = &toolCtxKey{"sessionKey"}
)

// WithToolContext returns a child context carrying channel and chatID.
func WithToolContext(ctx context.Context, channel, chatID string) context.Context {
	return WithToolMessageContext(ctx, channel, chatID, "", "")
}

// WithToolMessageContext returns a child context carrying message routing and source identifiers.
func WithToolMessageContext(ctx context.Context, channel, chatID, messageID, senderID string) context.Context {
	return WithToolExecutionContext(ctx, channel, chatID, messageID, senderID, "")
}

// WithToolExecutionContext returns a child context carrying request-scoped routing, source, and session identifiers.
func WithToolExecutionContext(
	ctx context.Context,
	channel, chatID, messageID, senderID, sessionKey string,
) context.Context {
	ctx = context.WithValue(ctx, ctxKeyChannel, channel)
	ctx = context.WithValue(ctx, ctxKeyChatID, chatID)
	ctx = context.WithValue(ctx, ctxKeyMessageID, messageID)
	ctx = context.WithValue(ctx, ctxKeySenderID, senderID)
	ctx = context.WithValue(ctx, ctxKeySessionKey, sessionKey)
	return ctx
}

// ToolChannel extracts the channel from ctx, or "" if unset.
func ToolChannel(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyChannel).(string)
	return v
}

// ToolChatID extracts the chatID from ctx, or "" if unset.
func ToolChatID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyChatID).(string)
	return v
}

// ToolMessageID extracts the current inbound platform message ID from ctx, or "" if unset.
func ToolMessageID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyMessageID).(string)
	return v
}

// ToolSenderID extracts the current inbound raw sender ID from ctx, or "" if unset.
func ToolSenderID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeySenderID).(string)
	return v
}

// ToolSessionKey extracts the current routed session key from ctx, or "" if unset.
func ToolSessionKey(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeySessionKey).(string)
	return v
}

func toolReplyTarget(ctx context.Context, channel, chatID string) (string, string) {
	channel = strings.TrimSpace(channel)
	chatID = strings.TrimSpace(chatID)
	if channel == "" || chatID == "" {
		return "", ""
	}
	if strings.TrimSpace(ToolChannel(ctx)) != channel || strings.TrimSpace(ToolChatID(ctx)) != chatID {
		return "", ""
	}
	return strings.TrimSpace(ToolMessageID(ctx)), strings.TrimSpace(ToolSenderID(ctx))
}

// AsyncCallback is a function type that async tools use to notify completion.
// When an async tool finishes its work, it calls this callback with the result.
//
// The ctx parameter allows the callback to be canceled if the agent is shutting down.
// The result parameter contains the tool's execution result.
//
// Example usage in an async tool:
//
//	func (t *MyAsyncTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
//	    // Start async work in background
//	    go func() {
//	        result := doAsyncWork()
//	        if t.callback != nil {
//	            t.callback(ctx, result)
//	        }
//	    }()
//	    return AsyncResult("Async task started")
//	}
type AsyncCallback func(ctx context.Context, result *ToolResult)

// AsyncTool is an optional interface that tools can implement to support
// asynchronous execution with completion callbacks.
//
// Async tools return immediately with an AsyncResult, then notify completion
// via the callback set by SetCallback.
//
// This is useful for:
// - Long-running operations that shouldn't block the agent loop
// - Subagent spawns that complete independently
// - Background tasks that need to report results later
//
// Example:
//
//	type SpawnTool struct {
//	    callback AsyncCallback
//	}
//
//	func (t *SpawnTool) SetCallback(cb AsyncCallback) {
//	    t.callback = cb
//	}
//
//	func (t *SpawnTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
//	    go t.runSubagent(ctx, args)
//	    return AsyncResult("Subagent spawned, will report back")
//	}
type AsyncTool interface {
	Tool
	// SetCallback registers a callback function to be invoked when the async operation completes.
	// The callback will be called from a goroutine and should handle thread-safety if needed.
	SetCallback(cb AsyncCallback)
}

// AsyncExecutor is a safer async interface that receives the completion callback
// per invocation instead of storing it on the tool instance.
type AsyncExecutor interface {
	Tool
	ExecuteAsync(ctx context.Context, args map[string]any, cb AsyncCallback) *ToolResult
}

func ToolToSchema(tool Tool) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        tool.Name(),
			"description": tool.Description(),
			"parameters":  tool.Parameters(),
		},
	}
}
