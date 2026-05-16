package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// discordCreds maps the credentials JSON from the channel_instances table.
type discordCreds struct {
	Token string `json:"token"`
}

// discordInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type discordInstanceConfig struct {
	DMPolicy          string   `json:"dm_policy,omitempty"`
	GroupPolicy       string   `json:"group_policy,omitempty"`
	AllowFrom         []string `json:"allow_from,omitempty"`
	AllowedChannels   []string `json:"allowed_channels,omitempty"`
	ChannelAgentRoutes map[string]string `json:"channel_agent_routes,omitempty"`
	RequireMention    *bool    `json:"require_mention,omitempty"`
	HistoryLimit      int      `json:"history_limit,omitempty"`
	BlockReply        *bool    `json:"block_reply,omitempty"`
	MediaMaxBytes     int64    `json:"media_max_bytes,omitempty"`
	STTProxyURL       string   `json:"stt_proxy_url,omitempty"`
	STTAPIKey         string   `json:"stt_api_key,omitempty"`
	STTTenantID       string   `json:"stt_tenant_id,omitempty"`
	STTTimeoutSeconds int      `json:"stt_timeout_seconds,omitempty"`
	VoiceAgentID      string   `json:"voice_agent_id,omitempty"`
}

// Factory creates a Discord channel from DB instance data (no extra stores).
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
	return buildChannel(name, creds, cfg, msgBus, pairingSvc, nil, nil, nil, nil)
}

// FactoryWithStores returns a ChannelFactory that includes agent, configPerm, and pending message stores.
func FactoryWithStores(agentStore store.AgentStore, configPermStore store.ConfigPermissionStore, pendingStore store.PendingMessageStore) channels.ChannelFactory {
	return FactoryWithStoresAndAudio(agentStore, configPermStore, pendingStore, nil)
}

// FactoryWithStoresAndAudio returns a ChannelFactory with all stores and STT support.
func FactoryWithStoresAndAudio(agentStore store.AgentStore, configPermStore store.ConfigPermissionStore, pendingStore store.PendingMessageStore, audioMgr *audio.Manager) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
		return buildChannel(name, creds, cfg, msgBus, pairingSvc, agentStore, configPermStore, pendingStore, audioMgr)
	}
}

func buildChannel(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore,
	agentStore store.AgentStore, configPermStore store.ConfigPermissionStore,
	pendingStore store.PendingMessageStore, audioMgr *audio.Manager) (channels.Channel, error) {

	var c discordCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode discord credentials: %w", err)
		}
	}
	if c.Token == "" {
		return nil, fmt.Errorf("discord token is required")
	}

	var ic discordInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode discord config: %w", err)
		}
	}

	dcCfg := config.DiscordConfig{
		Enabled:           true,
		Token:             c.Token,
		AllowFrom:         ic.AllowFrom,
		AllowedChannels:   ic.AllowedChannels,
		ChannelAgentRoutes: resolveDiscordChannelAgentRoutes(context.Background(), agentStore, ic.ChannelAgentRoutes),
		DMPolicy:          ic.DMPolicy,
		GroupPolicy:       ic.GroupPolicy,
		RequireMention:    ic.RequireMention,
		HistoryLimit:      ic.HistoryLimit,
		BlockReply:        ic.BlockReply,
		MediaMaxBytes:     ic.MediaMaxBytes,
		STTProxyURL:       ic.STTProxyURL,
		STTAPIKey:         ic.STTAPIKey,
		STTTenantID:       ic.STTTenantID,
		STTTimeoutSeconds: ic.STTTimeoutSeconds,
		VoiceAgentID:      ic.VoiceAgentID,
	}

	// DB instances default to "pairing" for groups (secure by default).
	if dcCfg.GroupPolicy == "" {
		dcCfg.GroupPolicy = "pairing"
	}

	ch, err := New(dcCfg, msgBus, pairingSvc, agentStore, configPermStore, pendingStore, audioMgr)
	if err != nil {
		return nil, err
	}

	ch.SetName(name)
	return ch, nil
}

func resolveDiscordChannelAgentRoutes(ctx context.Context, agentStore store.AgentStore, routes map[string]string) map[string]string {
	if len(routes) == 0 {
		return nil
	}
	resolved := make(map[string]string, len(routes))
	for channelID, agentRef := range routes {
		channelID = strings.TrimSpace(channelID)
		agentRef = strings.TrimSpace(agentRef)
		if channelID == "" || agentRef == "" {
			continue
		}
		if agentStore != nil {
			if id, err := uuid.Parse(agentRef); err == nil {
				if ag, err := agentStore.GetByID(ctx, id); err == nil && ag != nil && ag.AgentKey != "" {
					resolved[channelID] = ag.AgentKey
					continue
				}
			}
		}
		resolved[channelID] = agentRef
	}
	if len(resolved) == 0 {
		return nil
	}
	return resolved
}
