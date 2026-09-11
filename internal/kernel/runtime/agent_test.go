package runtime

import (
	"reflect"
	"testing"
)

func TestAgentCapabilityIndexAndVersionPinning(t *testing.T) {
	snapshot := NewTenantSnapshot(TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Version: 4,
		Agents: []Agent{
			{ID: "agent-a", ProjectID: "project", Name: "A", Status: "active", CurrentVersion: "v2", Capabilities: []string{"invoice.read", "chat"}},
			{ID: "agent-b", ProjectID: "project", Name: "B", Status: "active", Capabilities: []string{"invoice.read"}},
		},
		AgentEndpoints: []AgentEndpoint{
			{ID: "ep-a-v2", AgentID: "agent-a", Version: "v2", URL: "https://a.example", Protocol: "a2a", Capabilities: []string{"invoice.read"}},
			{ID: "ep-a-v1", AgentID: "agent-a", Version: "v1", URL: "https://a-old.example", Protocol: "a2a"},
		},
	})

	// Capability index is compiled from agents and endpoints.
	if got := snapshot.AgentsByCapability("invoice.read"); !reflect.DeepEqual(got, []string{"agent-a", "agent-b"}) {
		t.Fatalf("capability index = %v", got)
	}
	if got := snapshot.AgentsByCapability("chat"); !reflect.DeepEqual(got, []string{"agent-a"}) {
		t.Fatalf("chat index = %v", got)
	}

	// Version pinning: agent-a is pinned to v2; endpoint lookup by id.
	agent, ok := snapshot.Agent("agent-a")
	if !ok || agent.CurrentVersion != "v2" {
		t.Fatalf("agent = %+v", agent)
	}
	endpoint, ok := snapshot.AgentEndpoint("ep-a-v2")
	if !ok || endpoint.Version != "v2" || endpoint.Protocol != "a2a" {
		t.Fatalf("endpoint = %+v", endpoint)
	}
	if len(snapshot.AgentEndpoints()) != 2 {
		t.Fatalf("endpoints = %d", len(snapshot.AgentEndpoints()))
	}

	// Immutability: mutating the returned agent must not affect the index.
	agent.Capabilities[0] = "mutated"
	if got := snapshot.AgentsByCapability("invoice.read"); !reflect.DeepEqual(got, []string{"agent-a", "agent-b"}) {
		t.Fatalf("capability index was mutable: %v", got)
	}
}
