package agent

import (
	"context"
	"strings"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/tools"
)

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
}

func agentIDOrUnknown(agent *AgentInstance) string {
	if agent == nil {
		return "unknown"
	}
	return agent.ID
}
