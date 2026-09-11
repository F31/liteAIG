package admission

import (
	"context"
	"github.com/F31/liteAIG/internal/access/auth"
	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"strings"
	"testing"
	"time"
)

type ids struct{}

func (ids) New() (string, error) { return "request-1", nil }

type clock struct{}

func (clock) Now() time.Time { return time.Unix(1, 0) }

type authenticator struct {
	result *auth.Result
	err    error
}

func (a authenticator) Authenticate(context.Context, string, auth.RequestScope) (*auth.Result, error) {
	return a.result, a.err
}
func TestAdmissionBuildsTrustedContext(t *testing.T) {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref"})
	service, err := New(Config{MaxBodyBytes: 100}, authenticator{result: &auth.Result{Principal: identity.Principal{Type: "application", TenantID: "tenant", ProjectID: "project", APIKeyID: "key", ApplicationID: "app"}, Snapshot: snapshot, Key: runtime.APIKey{ModelAllowlist: []string{"chat"}}}}, ids{}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := service.Admit(context.Background(), Input{BodyBytes: 10, Request: &interaction.UnifiedRequest{Model: "chat", SessionID: "session", TaskID: "task", RootTaskID: "root", ParentTaskID: "parent"}})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.RequestID != "request-1" || ctx.Interaction.TenantID != "tenant" || ctx.Interaction.Caller.ID != "app" || ctx.Snapshot != snapshot {
		t.Fatalf("context=%+v", ctx)
	}
	if ctx.Interaction.TaskID != "task" || ctx.Interaction.RootTaskID != "root" || ctx.Interaction.ParentTaskID != "parent" || ctx.Interaction.SessionID != "session" {
		t.Fatalf("task context=%+v", ctx.Interaction)
	}
}

func TestAdmissionResolvesCanonicalAgentPrincipal(t *testing.T) {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Agents: []runtime.Agent{{ID: "agent", ProjectID: "project", Name: "Agent", Status: "active"}}})
	service, _ := New(Config{MaxBodyBytes: 100}, authenticator{result: &auth.Result{Principal: identity.Principal{Type: identity.PrincipalAgent, TenantID: "tenant", ProjectID: "project", AgentID: "agent"}, Snapshot: snapshot, Key: runtime.APIKey{ModelAllowlist: []string{"chat"}}}}, ids{}, clock{})
	ctx, err := service.Admit(context.Background(), Input{BodyBytes: 1, Request: &interaction.UnifiedRequest{Model: "chat"}})
	if err != nil || ctx.Interaction.Caller.Type != identity.PrincipalAgent || ctx.Interaction.Caller.ID != "agent" {
		t.Fatalf("agent context=%+v err=%v", ctx, err)
	}
	if agent, ok := snapshot.Agent("agent"); !ok || agent.Name != "Agent" {
		t.Fatalf("agent index=%+v", agent)
	}
}
func TestAdmissionRejectsBodyAndModelBeforePipeline(t *testing.T) {
	service, _ := New(Config{MaxBodyBytes: 5}, authenticator{result: &auth.Result{Principal: identity.Principal{}, Snapshot: &runtime.TenantRuntimeSnapshot{}, Key: runtime.APIKey{ModelAllowlist: []string{"allowed"}}}}, ids{}, clock{})
	if _, err := service.Admit(context.Background(), Input{BodyBytes: 6, Request: &interaction.UnifiedRequest{Model: "allowed"}}); err == nil || !strings.Contains(err.Error(), "REQUEST_TOO_LARGE") {
		t.Fatalf("body error=%v", err)
	}
	if _, err := service.Admit(context.Background(), Input{BodyBytes: 1, Request: &interaction.UnifiedRequest{Model: "denied"}}); err == nil || !strings.Contains(err.Error(), "MODEL_FORBIDDEN") {
		t.Fatalf("model error=%v", err)
	}
}
