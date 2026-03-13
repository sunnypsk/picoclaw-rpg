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

type proactiveOutputCapture struct {
	mu       sync.Mutex
	contents []string
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
	registry *AgentRegistry,
	agentID, channel, chatID string,
	proactive bool,
) {
	if registry == nil {
		return
	}
	agent, ok := registry.GetAgent(agentID)
	if !ok {
		return
	}
	if err := recordNPCOutboundMessage(agent, channel, chatID); err != nil {
		logger.WarnCF("agent", "Failed to record outbound message state", map[string]any{
			"agent_id":  agentID,
			"channel":   channel,
			"chat_id":   chatID,
			"proactive": proactive,
			"error":     err.Error(),
		})
	}
}

func (al *AgentLoop) publishAgentMessage(
	ctx context.Context,
	agent *AgentInstance,
	channel, chatID, content string,
	proactive bool,
) {
	if al == nil || al.bus == nil {
		return
	}
	if strings.TrimSpace(channel) == "" || strings.TrimSpace(chatID) == "" || content == "" {
		return
	}
	if err := al.bus.PublishOutbound(ctx, bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: content,
	}); err != nil {
		logger.DebugCF("agent", "Skipped outbound publish", map[string]any{
			"agent_id":  agentIDOrUnknown(agent),
			"channel":   channel,
			"chat_id":   chatID,
			"proactive": proactive,
			"error":     err.Error(),
		})
		return
	}
	if err := recordNPCOutboundMessage(agent, channel, chatID); err != nil {
		logger.WarnCF("agent", "Failed to record outbound message state", map[string]any{
			"agent_id":  agentIDOrUnknown(agent),
			"channel":   channel,
			"chat_id":   chatID,
			"proactive": proactive,
			"error":     err.Error(),
		})
	}
	if proactive {
		al.appendVisibleAssistantMessagesToSession(agent, proactiveSessionKeyFromContext(ctx), channel, chatID, []string{content})
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
