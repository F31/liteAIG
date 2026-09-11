// Package federation owns cross-organization Agent trust, including task
// governance counters for federated calls.
package federation

import (
	"sync"
)

// ConsistencyMode selects how task counters are enforced across regions.
type ConsistencyMode string

const (
	ConsistencyRegional   ConsistencyMode = "regional"
	ConsistencyGlobalSoft ConsistencyMode = "global_soft"
	ConsistencyGlobalHard ConsistencyMode = "global_hard"
)

// TaskLimits bound a task's federated footprint.
type TaskLimits struct {
	MaxAgentHops  int
	MaxAgentCalls int
	MaxTotalCost  float64
	Consistency   ConsistencyMode
}

// TaskState tracks a task's counters and the sticky boundary flag.
type TaskState struct {
	mu sync.Mutex

	Hops            int
	Calls           int
	TotalCost       float64
	crossedBoundary bool
}

// RecordCrossing sets the sticky flag; it never resets.
func (t *TaskState) RecordCrossing() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.crossedBoundary = true
}

// CrossedTrustBoundary reports whether the task ever crossed a boundary.
func (t *TaskState) CrossedTrustBoundary() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.crossedBoundary
}

// ShouldAllowHop evaluates hop/call/cost limits for the next downstream call.
// Regional and global_hard are strict; global_soft allows a small bounded
// overshoot slice (2 hops / 2 calls / 1.1x cost) that is reconciled later.
func (t *TaskState) ShouldAllowHop(limits TaskLimits) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if limits.Consistency != ConsistencyGlobalSoft {
		hopsOK := limits.MaxAgentHops <= 0 || t.Hops < limits.MaxAgentHops
		callsOK := limits.MaxAgentCalls <= 0 || t.Calls < limits.MaxAgentCalls
		costOK := limits.MaxTotalCost <= 0 || t.TotalCost < limits.MaxTotalCost
		return hopsOK && callsOK && costOK
	}

	hopsOK := limits.MaxAgentHops <= 0 || t.Hops < limits.MaxAgentHops+2
	callsOK := limits.MaxAgentCalls <= 0 || t.Calls < limits.MaxAgentCalls+2
	costOK := limits.MaxTotalCost <= 0 || t.TotalCost < limits.MaxTotalCost*1.1
	return hopsOK && callsOK && costOK
}

// ConsumeHop advances the counters for an admitted downstream call.
func (t *TaskState) ConsumeHop(limits TaskLimits, cost float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Hops++
	t.Calls++
	t.TotalCost += cost
}

// LoopDetector terminates an anomalous agent call chain.
type LoopDetector struct {
	mu       sync.Mutex
	path     []string
	maxDepth int
}

// NewLoopDetector builds a detector that terminates at maxDepth.
func NewLoopDetector(maxDepth int) *LoopDetector {
	if maxDepth <= 0 {
		maxDepth = 8
	}
	return &LoopDetector{maxDepth: maxDepth}
}

// Enter records an agent hop and reports whether the chain should continue.
func (d *LoopDetector) Enter(agentID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, visited := range d.path {
		if visited == agentID {
			return false // loop detected
		}
	}
	if len(d.path) >= d.maxDepth {
		return false
	}
	d.path = append(d.path, agentID)
	return true
}

// Exit removes the most recent hop.
func (d *LoopDetector) Exit() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.path) > 0 {
		d.path = d.path[:len(d.path)-1]
	}
}
