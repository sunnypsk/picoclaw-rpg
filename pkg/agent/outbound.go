package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type proactiveContextKey string

const proactiveOutputCaptureKey proactiveContextKey = "proactive_output_capture"
const proactiveSessionKeyContextKey proactiveContextKey = "proactive_session_key"
const replyStateTrackerContextKey proactiveContextKey = "reply_state_tracker"
const replyStateTrackerDisabledKey proactiveContextKey = "reply_state_tracker_disabled"

type proactiveOutputCapture struct {
	mu       sync.Mutex
	contents []string
}

type replyStateTracker struct {
	mu          sync.Mutex
	lastContent string
}

func withProactiveOutputCapture(ctx context.Context, capture *proactiveOutputCapture) context.Context {
	if ctx == nil || capture == nil {
		return ctx
	}
	return context.WithValue(ctx, proactiveOutputCaptureKey, capture)
}

func proactiveOutputCaptureFromContext(ctx context.Context) *proactiveOutputCapture {
	if ctx == nil {
		return nil
	}
	capture, _ := ctx.Value(proactiveOutputCaptureKey).(*proactiveOutputCapture)
	return capture
}

func withMirroredOutboundCapture(ctx context.Context, capture *proactiveOutputCapture) context.Context {
	return withProactiveOutputCapture(ctx, capture)
}

func mirroredOutboundCaptureFromContext(ctx context.Context) *proactiveOutputCapture {
	return proactiveOutputCaptureFromContext(ctx)
}

func withProactiveSessionKey(ctx context.Context, sessionKey string) context.Context {
	if ctx == nil || strings.TrimSpace(sessionKey) == "" {
		return ctx
	}
	return context.WithValue(ctx, proactiveSessionKeyContextKey, strings.TrimSpace(sessionKey))
}

func proactiveSessionKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sessionKey, _ := ctx.Value(proactiveSessionKeyContextKey).(string)
	return strings.TrimSpace(sessionKey)
}

func withMirroredSessionKey(ctx context.Context, sessionKey string) context.Context {
	return withProactiveSessionKey(ctx, sessionKey)
}

func mirroredSessionKeyFromContext(ctx context.Context) string {
	return proactiveSessionKeyFromContext(ctx)
}

func withReplyStateTracker(ctx context.Context, tracker *replyStateTracker) context.Context {
	if ctx == nil || tracker == nil {
		return ctx
	}
	return context.WithValue(ctx, replyStateTrackerContextKey, tracker)
}

func replyStateTrackerFromContext(ctx context.Context) *replyStateTracker {
	if ctx == nil {
		return nil
	}
	tracker, _ := ctx.Value(replyStateTrackerContextKey).(*replyStateTracker)
	return tracker
}

func withoutReplyStateTracking(ctx context.Context) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, replyStateTrackerDisabledKey, true)
}

func replyStateTrackingDisabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	disabled, _ := ctx.Value(replyStateTrackerDisabledKey).(bool)
	return disabled
}

func (c *proactiveOutputCapture) Add(content string) {
	if c == nil || strings.TrimSpace(content) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.contents = append(c.contents, content)
}

func (c *proactiveOutputCapture) Messages() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.contents))
	copy(out, c.contents)
	return out
}

func (t *replyStateTracker) Record(content string) {
	if t == nil {
		return
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastContent = trimmed
}

func (t *replyStateTracker) LastContent() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastContent
}

func agentMessageAlreadySent(agent *AgentInstance) bool {
	if agent == nil {
		return false
	}
	tool, ok := agent.Tools.Get("message")
	if !ok {
		return false
	}
	mt, ok := tool.(*tools.MessageTool)
	if !ok {
		return false
	}
	return mt.HasSentInRound()
}

func recordOutboundMessageForRegistry(
	ctx context.Context,
	registry *AgentRegistry,
	agentID, channel, chatID, content string,
) {
	if registry == nil {
		return
	}
	agent, ok := registry.GetAgent(agentID)
	if !ok {
		return
	}
	recordSuccessfulOutbound(ctx, agent, channel, chatID, content)
}

func (al *AgentLoop) publishAgentMessage(
	ctx context.Context,
	agent *AgentInstance,
	channel, chatID, content string,
	mirrorToSession bool,
) bool {
	if al == nil || al.bus == nil {
		return false
	}
	if strings.TrimSpace(channel) == "" || strings.TrimSpace(chatID) == "" || content == "" {
		return false
	}
	if err := al.bus.PublishOutbound(ctx, bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: content,
	}); err != nil {
		logger.DebugCF("agent", "Skipped outbound publish", map[string]any{
			"agent_id":          agentIDOrUnknown(agent),
			"channel":           channel,
			"chat_id":           chatID,
			"mirror_to_session": mirrorToSession,
			"error":             err.Error(),
		})
		return false
	}
	recordSuccessfulOutbound(ctx, agent, channel, chatID, content)
	if mirrorToSession {
		al.appendVisibleAssistantMessagesToSession(agent, mirroredSessionKeyFromContext(ctx), channel, chatID, []string{content})
	}
	return true
}

func recordSuccessfulOutbound(
	ctx context.Context,
	agent *AgentInstance,
	channel, chatID, content string,
) {
	if capture := mirroredOutboundCaptureFromContext(ctx); capture != nil {
		capture.Add(content)
		if err := recordNPCOutboundMessage(agent, channel, chatID); err != nil {
			logger.WarnCF("agent", "Failed to record outbound message state", map[string]any{
				"agent_id": agentIDOrUnknown(agent),
				"channel":  channel,
				"chat_id":  chatID,
				"error":    err.Error(),
			})
		}
		return
	}

	if replyStateTrackingDisabled(ctx) {
		return
	}

	if tracker := replyStateTrackerFromContext(ctx); tracker != nil {
		tracker.Record(content)
	}
}

func (al *AgentLoop) appendVisibleAssistantMessagesToSession(
	agent *AgentInstance,
	sessionKey, channel, chatID string,
	contents []string,
) {
	if al == nil || agent == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	added := false
	for _, content := range contents {
		trimmed := strings.TrimSpace(content)
		if trimmed == "" {
			continue
		}
		agent.Sessions.AddMessage(sessionKey, "assistant", trimmed)
		added = true
	}
	if !added {
		return
	}
	agent.Sessions.Save(sessionKey)
	al.maybeSummarize(agent, sessionKey, channel, chatID)
}

func agentIDOrUnknown(agent *AgentInstance) string {
	if agent == nil {
		return "unknown"
	}
	return agent.ID
}
