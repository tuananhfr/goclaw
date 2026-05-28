package discord

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type routeAgentStore struct {
	store.AgentStore
	agentID uuid.UUID
	key     string
}

func (s routeAgentStore) GetByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	if id != s.agentID {
		return nil, nil
	}
	return &store.AgentData{BaseModel: store.BaseModel{ID: id}, AgentKey: s.key}, nil
}

func TestResolveDiscordChannelAgentRoutes(t *testing.T) {
	agentID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got := resolveDiscordChannelAgentRoutes(context.Background(), routeAgentStore{
		agentID: agentID,
		key:     "pizza-agent",
	}, map[string]string{
		" chan-1 ": agentID.String(),
		"chan-2":   "pen-pot-agent",
		"chan-3":   "",
		"":         "ignored-agent",
	})

	if got["chan-1"] != "pizza-agent" {
		t.Fatalf("UUID route resolved to %q, want pizza-agent", got["chan-1"])
	}
	if got["chan-2"] != "pen-pot-agent" {
		t.Fatalf("agent_key route resolved to %q, want pen-pot-agent", got["chan-2"])
	}
	if _, ok := got["chan-3"]; ok {
		t.Fatal("empty agent route should be dropped")
	}
}
