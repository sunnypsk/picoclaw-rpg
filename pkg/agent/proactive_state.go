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
	relationshipKey := buildRelationshipKey(msg.Channel, msg.SenderID)
	if relationshipKey == "" {
		return nil
	}

	return agent.StateStore.UpdateState(func(state *NPCState) (bool, error) {
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

		return true, nil
	})
}

func recordRelationshipSessionKey(agent *AgentInstance, msg bus.InboundMessage, sessionKey string) error {
	if !shouldTrackRelationshipMessage(msg) {
		return nil
	}
	relationshipKey := buildRelationshipKey(msg.Channel, msg.SenderID)
	if relationshipKey == "" {
		return nil
	}
	if agent == nil || agent.StateStore == nil {
		return nil
	}
	return agent.StateStore.UpdateState(func(state *NPCState) (bool, error) {
		if state.Relationships == nil {
			state.Relationships = make(map[string]NPCRelationship)
		}
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
		state.Relationships[relationshipKey] = rel
		return true, nil
	})
}

func recordMinimalRelationshipTurn(agent *AgentInstance, msg bus.InboundMessage, assistantReply string) error {
	if !shouldTrackRelationshipMessage(msg) {
		return nil
	}
	if agent == nil || agent.StateStore == nil {
		return nil
	}

	return agent.StateStore.UpdateState(func(state *NPCState) (bool, error) {
		previousLocation := state.Location
		applyMinimalTurnUpdate(state, msg, assistantReply)
		preserveActiveOutingDuringChat(previousLocation, &state.Location, time.Now())
		return true, nil
	})
}

func recordNPCOutboundMessage(agent *AgentInstance, channel, chatID string) error {
	if agent == nil || agent.StateStore == nil {
		return nil
	}

	return agent.StateStore.UpdateState(func(state *NPCState) (bool, error) {
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
		return updated, nil
	})
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
	key := strings.ToLower(strings.TrimSpace(relationshipKey))
	return agent.StateStore.UpdateState(func(state *NPCState) (bool, error) {
		rel, ok := state.Relationships[key]
		if !ok {
			return false, nil
		}
		update(&rel)
		state.Relationships[key] = rel
		return true, nil
	})
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
