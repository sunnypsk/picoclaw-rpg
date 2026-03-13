package agent

import (
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/constants"
)

func prepareRelationshipTarget(agent *AgentInstance, msg bus.InboundMessage, sessionKey string) error {
	if !shouldTrackRelationshipMessage(msg) {
		return nil
	}
	if agent == nil || agent.StateStore == nil {
		return nil
	}
	state, err := agent.StateStore.LoadState()
	if err != nil {
		return err
	}
	state = normalizeNPCState(state)
	if state.Relationships == nil {
		state.Relationships = make(map[string]NPCRelationship)
	}
	relationshipKey := buildRelationshipKey(msg.Channel, msg.SenderID)
	if relationshipKey == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rel, ok := state.Relationships[relationshipKey]
	if !ok {
		rel = NPCRelationship{
			Affinity:    NPCLevelMid,
			Trust:       NPCLevelMid,
			Familiarity: NPCLevelLow,
		}
	}
	rel.LastChannel = normalizeRelationshipChannel(msg.Channel)
	rel.LastChatID = strings.TrimSpace(msg.ChatID)
	rel.LastPeerKind = normalizeRelationshipPeerKind(msg.Peer.Kind)
	rel.LastSessionKey = strings.TrimSpace(sessionKey)
	rel.LastUserMessageAt = now
	rel.LastInteractionAt = now
	state.Relationships[relationshipKey] = rel
	return agent.StateStore.SaveState(state)
}

func recordRelationshipSessionKey(agent *AgentInstance, msg bus.InboundMessage, sessionKey string) error {
	if !shouldTrackRelationshipMessage(msg) {
		return nil
	}
	relationshipKey := buildRelationshipKey(msg.Channel, msg.SenderID)
	if relationshipKey == "" {
		return nil
	}
	return updateRelationshipState(agent, relationshipKey, func(rel *NPCRelationship) {
		rel.LastChannel = normalizeRelationshipChannel(msg.Channel)
		rel.LastChatID = strings.TrimSpace(msg.ChatID)
		rel.LastPeerKind = normalizeRelationshipPeerKind(msg.Peer.Kind)
		rel.LastSessionKey = strings.TrimSpace(sessionKey)
	})
}

func recordMinimalRelationshipTurn(agent *AgentInstance, msg bus.InboundMessage, assistantReply string) error {
	if !shouldTrackRelationshipMessage(msg) {
		return nil
	}
	if agent == nil || agent.StateStore == nil {
		return nil
	}
	state, err := agent.StateStore.LoadState()
	if err != nil {
		return err
	}
	previousState := normalizeNPCState(state)
	nextState := previousState
	applyMinimalTurnUpdate(&nextState, msg, assistantReply)
	preserveActiveOutingDuringChat(previousState.Location, &nextState.Location, time.Now())
	return agent.StateStore.SaveState(nextState)
}

func recordNPCOutboundMessage(agent *AgentInstance, channel, chatID string) error {
	if agent == nil || agent.StateStore == nil {
		return nil
	}
	state, err := agent.StateStore.LoadState()
	if err != nil {
		return err
	}
	state = normalizeNPCState(state)
	updated := false
	now := time.Now().UTC().Format(time.RFC3339)
	normalizedChannel := normalizeRelationshipChannel(channel)
	normalizedChatID := strings.TrimSpace(chatID)
	for key, rel := range state.Relationships {
		if rel.LastChannel != normalizedChannel || strings.TrimSpace(rel.LastChatID) != normalizedChatID {
			continue
		}
		rel.LastAgentMessageAt = now
		state.Relationships[key] = rel
		updated = true
	}
	if !updated {
		return nil
	}
	return agent.StateStore.SaveState(state)
}

func recordProactiveAttempt(agent *AgentInstance, relationshipKey string, at time.Time) error {
	return updateRelationshipState(agent, relationshipKey, func(rel *NPCRelationship) {
		rel.LastProactiveAttemptAt = at.UTC().Format(time.RFC3339)
	})
}

func recordProactiveSuccess(agent *AgentInstance, relationshipKey string, at time.Time) error {
	return updateRelationshipState(agent, relationshipKey, func(rel *NPCRelationship) {
		timestamp := at.UTC().Format(time.RFC3339)
		rel.LastAgentMessageAt = timestamp
		rel.LastProactiveSuccessAt = timestamp
	})
}

func updateRelationshipState(agent *AgentInstance, relationshipKey string, update func(rel *NPCRelationship)) error {
	if agent == nil || agent.StateStore == nil || strings.TrimSpace(relationshipKey) == "" || update == nil {
		return nil
	}
	state, err := agent.StateStore.LoadState()
	if err != nil {
		return err
	}
	state = normalizeNPCState(state)
	if state.Relationships == nil {
		state.Relationships = make(map[string]NPCRelationship)
	}
	key := strings.ToLower(strings.TrimSpace(relationshipKey))
	rel, ok := state.Relationships[key]
	if !ok {
		return nil
	}
	update(&rel)
	state.Relationships[key] = rel
	return agent.StateStore.SaveState(state)
}

func normalizeRelationshipChannel(channel string) string {
	return strings.ToLower(strings.TrimSpace(channel))
}

func normalizeRelationshipPeerKind(peerKind string) string {
	return strings.ToLower(strings.TrimSpace(peerKind))
}

func shouldTrackRelationshipMessage(msg bus.InboundMessage) bool {
	if constants.IsInternalChannel(msg.Channel) {
		return false
	}
	senderID := strings.ToLower(strings.TrimSpace(msg.SenderID))
	if senderID == "" {
		return false
	}
	switch senderID {
	case "cron", "heartbeat", "system", "subagent":
		return false
	default:
		return true
	}
}
