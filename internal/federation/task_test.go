package federation

import "testing"

func TestEffectivePermissionIntersection(t *testing.T) {
	base := PermissionRequest{
		SystemPolicyEnabled: true, TenantActive: true, ProjectActive: true,
		CallerAllowed: true, LocalDelegationGranted: true, RelationshipActive: true,
		ProjectGranted: true, CapabilityGranted: true, DataBoundarySatisfied: true,
	}
	if !Evaluate(base).Allowed() {
		t.Fatal("full intersection should allow")
	}
	// Any single failing factor denies the call.
	factors := []struct {
		name   string
		mutate func(*PermissionRequest)
	}{
		{"relationship", func(p *PermissionRequest) { p.RelationshipActive = false }},
		{"project_grant", func(p *PermissionRequest) { p.ProjectGranted = false }},
		{"capability_grant", func(p *PermissionRequest) { p.CapabilityGranted = false }},
		{"data_boundary", func(p *PermissionRequest) { p.DataBoundarySatisfied = false }},
		{"delegation", func(p *PermissionRequest) { p.LocalDelegationGranted = false }},
	}
	for _, factor := range factors {
		request := base
		factor.mutate(&request)
		if Evaluate(request).Allowed() {
			t.Fatalf("denied factor %s still allowed", factor.name)
		}
	}
}

func TestTaskCountersBlockNextCall(t *testing.T) {
	limits := TaskLimits{MaxAgentHops: 5, MaxAgentCalls: 5, MaxTotalCost: 100, Consistency: ConsistencyRegional}
	state := &TaskState{}
	if !state.ShouldAllowHop(limits) {
		t.Fatal("initial hop should be allowed")
	}
	state.ConsumeHop(limits, 40)
	state.ConsumeHop(limits, 40)
	if !state.ShouldAllowHop(limits) { // hops 2 < 5; cost 80 < 100
		t.Fatal("hop within limit should be allowed")
	}
	state.ConsumeHop(limits, 40) // cost 120 > 100
	if state.ShouldAllowHop(limits) {
		t.Fatal("cost limit must block the next call")
	}
	// Hop limit also blocks when reached.
	hopLimited := &TaskState{}
	hopLimits := TaskLimits{MaxAgentHops: 2, MaxAgentCalls: 10, MaxTotalCost: 1000, Consistency: ConsistencyRegional}
	hopLimited.ConsumeHop(hopLimits, 1)
	hopLimited.ConsumeHop(hopLimits, 1)
	if hopLimited.ShouldAllowHop(hopLimits) {
		t.Fatal("hop limit must block when reached")
	}
}

func TestGlobalSoftAllowsBoundedOvershoot(t *testing.T) {
	regional := TaskLimits{MaxAgentHops: 2, MaxTotalCost: 100, Consistency: ConsistencyRegional}
	soft := TaskLimits{MaxAgentHops: 2, MaxTotalCost: 100, Consistency: ConsistencyGlobalSoft}
	hard := TaskLimits{MaxAgentHops: 2, MaxTotalCost: 100, Consistency: ConsistencyGlobalHard}
	state := &TaskState{}
	state.ConsumeHop(soft, 100) // reaches the hard cost limit
	// global_soft allows a bounded overshoot slice; strict modes do not.
	if !state.ShouldAllowHop(soft) {
		t.Fatal("global_soft should allow bounded overshoot within 1.1x")
	}
	if state.ShouldAllowHop(regional) || state.ShouldAllowHop(hard) {
		t.Fatal("regional/global_hard must not overshoot")
	}
	// Beyond the bounded slice even global_soft blocks.
	state.ConsumeHop(soft, 100) // cost 200 > 110
	if state.ShouldAllowHop(soft) {
		t.Fatal("global_soft must block beyond the bounded slice")
	}
}

func TestStickyCrossesTrustBoundary(t *testing.T) {
	state := &TaskState{}
	if state.CrossedTrustBoundary() {
		t.Fatal("flag must start false")
	}
	state.RecordCrossing()
	state.RecordCrossing()
	if !state.CrossedTrustBoundary() {
		t.Fatal("flag must be sticky after a crossing")
	}
}

func TestLoopDetectorTerminates(t *testing.T) {
	detector := NewLoopDetector(8)
	if !detector.Enter("agent-a") || !detector.Enter("agent-b") {
		t.Fatal("first hops should be allowed")
	}
	detector.Exit()
	if !detector.Enter("agent-b") {
		t.Fatal("re-entering after exit should be allowed")
	}
	// Direct self-call loop.
	if detector.Enter("agent-b") {
		t.Fatal("self-call loop must be terminated")
	}
}
