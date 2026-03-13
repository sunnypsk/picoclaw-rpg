package agent

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
)

const (
	dailyLogEventSessionStarted     = "session_started"
	dailyLogEventSessionClosedByNew = "session_closed_by_new"
	dailyLogEventPreCompression     = "pre_compression"
)

type dailyLogEventLine struct {
	Type         string `json:"type"`
	Event        string `json:"event"`
	At           string `json:"at"`
	AgentID      string `json:"agent_id"`
	SessionKey   string `json:"session_key"`
	Channel      string `json:"channel,omitempty"`
	ChatID       string `json:"chat_id,omitempty"`
	MessageCount int    `json:"message_count"`
}

type dailyLogMessageLine struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (al *AgentLoop) sessionRotationKey(agentID, baseSessionKey string) string {
	return strings.ToLower(strings.TrimSpace(agentID)) + "|" + strings.ToLower(strings.TrimSpace(baseSessionKey))
}

func (al *AgentLoop) resolveSessionKey(route routing.ResolvedRoute, msg bus.InboundMessage) string {
	if msg.SessionKey != "" && strings.HasPrefix(msg.SessionKey, "agent:") {
		explicit := strings.ToLower(strings.TrimSpace(msg.SessionKey))
		agentID := route.AgentID
		if parsed := routing.ParseAgentSessionKey(explicit); parsed != nil {
			agentID = parsed.AgentID
		}
		return al.resolveRotatedSessionKey(agentID, explicit)
	}

	return al.resolveRotatedSessionKey(route.AgentID, route.SessionKey)
}

func (al *AgentLoop) resolveRotatedSessionKey(agentID, baseSessionKey string) string {
	base := strings.ToLower(strings.TrimSpace(baseSessionKey))
	if base == "" {
		return base
	}

	current := base
	path := []string{base}
	seen := map[string]struct{}{base: {}}
	cycleDetected := false

	for {
		rotated, ok := al.sessionRotates.Load(al.sessionRotationKey(agentID, current))
		if !ok {
			break
		}

		next, ok := rotated.(string)
		next = strings.ToLower(strings.TrimSpace(next))
		if !ok || next == "" {
			break
		}

		if _, exists := seen[next]; exists {
			cycleDetected = true
			logger.WarnCF("agent", "Detected session rotation cycle", map[string]any{
				"agent_id":         agentID,
				"base_session_key": base,
				"current":          current,
				"next":             next,
			})
			break
		}

		path = append(path, next)
		seen[next] = struct{}{}
		current = next
	}

	if !cycleDetected && current != base {
		for _, key := range path[:len(path)-1] {
			if key == current {
				continue
			}
			al.sessionRotates.Store(al.sessionRotationKey(agentID, key), current)
		}
	}

	return current
}

func (al *AgentLoop) setSessionRotation(agentID, baseSessionKey, rotatedSessionKey string) {
	base := strings.ToLower(strings.TrimSpace(baseSessionKey))
	rotated := strings.ToLower(strings.TrimSpace(rotatedSessionKey))
	if base == "" || rotated == "" {
		return
	}
	al.sessionRotates.Store(al.sessionRotationKey(agentID, base), rotated)
}

func buildRotatedSessionKey(baseSessionKey string) string {
	base := strings.ToLower(strings.TrimSpace(baseSessionKey))
	if base == "" {
		base = routing.BuildAgentMainSessionKey(routing.DefaultAgentID)
	}
	return fmt.Sprintf("%s:new:%d", base, time.Now().UTC().UnixNano())
}

func (al *AgentLoop) handleNewSessionCommand(
	agent *AgentInstance,
	route routing.ResolvedRoute,
	msg bus.InboundMessage,
	currentSessionKey string,
) (string, error) {
	if agent == nil {
		return "", fmt.Errorf("no agent available for /new")
	}

	history := agent.Sessions.GetHistory(currentSessionKey)
	if len(history) > 0 {
		al.appendDailyLogJSONL(agent, currentSessionKey, msg.Channel, msg.ChatID, dailyLogEventSessionClosedByNew, history)
	}

	rotationAgentID := route.AgentID
	if parsed := routing.ParseAgentSessionKey(currentSessionKey); parsed != nil {
		rotationAgentID = parsed.AgentID
	}

	newSessionKey := buildRotatedSessionKey(currentSessionKey)
	al.setSessionRotation(rotationAgentID, currentSessionKey, newSessionKey)
	al.appendDailyLogJSONL(agent, newSessionKey, msg.Channel, msg.ChatID, dailyLogEventSessionStarted, nil)
	if err := recordRelationshipSessionKey(agent, msg, newSessionKey); err != nil {
		logger.WarnCF("agent", "Failed to record rotated relationship session key", map[string]any{
			"agent_id":    agent.ID,
			"session_key": newSessionKey,
			"channel":     msg.Channel,
			"sender":      msg.SenderID,
			"error":       err.Error(),
		})
	}

	return fmt.Sprintf("Started a new session. New session key: %s", newSessionKey), nil
}

func (al *AgentLoop) appendDailyLogJSONL(
	agent *AgentInstance,
	sessionKey, channel, chatID, event string,
	segment []providers.Message,
) {
	if agent == nil || strings.TrimSpace(agent.Workspace) == "" || strings.TrimSpace(event) == "" {
		return
	}

	filtered := filterUserAssistantMessages(segment)
	dedupeKey := buildDailyLogDedupeKey(sessionKey, event, filtered)
	if !al.markDailyLogOnce(dedupeKey) {
		return
	}

	lines := make([]string, 0, 1+len(filtered))
	eventLine, err := json.Marshal(dailyLogEventLine{
		Type:         "session_event",
		Event:        event,
		At:           time.Now().UTC().Format(time.RFC3339),
		AgentID:      agent.ID,
		SessionKey:   sessionKey,
		Channel:      channel,
		ChatID:       chatID,
		MessageCount: len(filtered),
	})
	if err != nil {
		logger.WarnCF("agent", "Failed to encode daily log event line", map[string]any{"error": err.Error()})
		return
	}
	lines = append(lines, string(eventLine))

	for _, msg := range filtered {
		payload, err := json.Marshal(dailyLogMessageLine{
			Type:    "message",
			Role:    msg.Role,
			Content: msg.Content,
		})
		if err != nil {
			continue
		}
		lines = append(lines, string(payload))
	}

	store := NewMemoryStore(agent.Workspace)
	if err := store.AppendToday(strings.Join(lines, "\n")); err != nil {
		if strings.TrimSpace(dedupeKey) != "" {
			al.dailyLogDedupe.Delete(dedupeKey)
		}
		logger.WarnCF("agent", "Failed to append daily session log", map[string]any{
			"agent_id":    agent.ID,
			"session_key": sessionKey,
			"event":       event,
			"error":       err.Error(),
		})
		return
	}

	if agent.MemoryIndex != nil {
		go func() {
			syncCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = agent.MemoryIndex.Sync(syncCtx)
		}()
	}
}

func filterUserAssistantMessages(segment []providers.Message) []providers.Message {
	if len(segment) == 0 {
		return nil
	}

	filtered := make([]providers.Message, 0, len(segment))
	for _, msg := range segment {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		filtered = append(filtered, providers.Message{Role: msg.Role, Content: msg.Content})
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func buildDailyLogDedupeKey(sessionKey, event string, segment []providers.Message) string {
	event = strings.TrimSpace(event)
	sessionKey = strings.TrimSpace(sessionKey)
	if event == "" {
		return ""
	}

	if event == dailyLogEventSessionStarted {
		return event + ":" + sessionKey
	}

	h := sha1.New()
	_, _ = h.Write([]byte(event))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(sessionKey))
	_, _ = h.Write([]byte{0})
	for _, msg := range segment {
		_, _ = h.Write([]byte(msg.Role))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(msg.Content))
		_, _ = h.Write([]byte{0})
	}
	return event + ":" + sessionKey + ":" + hex.EncodeToString(h.Sum(nil))
}

func (al *AgentLoop) markDailyLogOnce(key string) bool {
	if strings.TrimSpace(key) == "" {
		return true
	}
	_, loaded := al.dailyLogDedupe.LoadOrStore(key, struct{}{})
	return !loaded
}
