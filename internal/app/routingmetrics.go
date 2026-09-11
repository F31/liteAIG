// Rolling per-deployment routing metrics (Stage 10): the Lite pipeline feeds
// observed latency/cost/success per selected deployment into a process-local
// 1-minute window. The soft routing strategy then scores candidates from real
// observations instead of degenerate flat values. EWMA smoothing (spec §8.2)
// keeps the signal stable across single-request spikes.
package app

import (
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	routing "github.com/F31/liteAIG/internal/routing/model"
)

// metricsWindow is how long observations are retained. Past the window a
// deployment falls back to no-data scoring (priority order).
const metricsWindow = time.Minute

// deploymentStats is one deployment's rolling observation window.
type deploymentStats struct {
	latencyEWMA   float64
	costEWMA      float64
	cacheAffinity float64 // EWMA of prompt-cache hit ratio (0..1), higher is better
	success       int64   // completed (non-error) requests
	failed        int64   // errored requests
	inflight      int64   // currently executing requests (load)
	last          time.Time
}

// routingMetrics holds per-deployment rolling observations.
type routingMetrics struct {
	clock contracts.Clock
	mu    sync.Mutex
	stats map[string]*deploymentStats
}

func newRoutingMetrics(clock contracts.Clock) *routingMetrics {
	return &routingMetrics{clock: clock, stats: map[string]*deploymentStats{}}
}

// begin marks a request as in-flight for a deployment (load signal).
func (m *routingMetrics) begin(deploymentID string) {
	m.mu.Lock()
	state := m.stateLocked(deploymentID)
	state.inflight++
	m.mu.Unlock()
}

// observe records one completed request. latencyMS, cost, and cacheHitRatio
// feed the EWMAs; success marks whether the request completed without an
// error. cacheHitRatio is the prompt-cache hit share (0..1) reported by the
// provider for the request, the real cache_affinity source that the soft
// routing score prefers (§11.3).
func (m *routingMetrics) observe(deploymentID string, latencyMS, cost, cacheHitRatio float64, success bool) {
	m.mu.Lock()
	state := m.stateLocked(deploymentID)
	if state.inflight > 0 {
		state.inflight--
	}
	prior := state.success + state.failed
	if success {
		state.success++
	} else {
		state.failed++
	}
	// EWMA with a short half-life (~10 samples) so the metric reacts to recent
	// traffic while damping single-request noise. The first observation seeds
	// the EMA directly to avoid a cold-start underweight.
	const alpha = 0.3
	if prior == 0 {
		state.latencyEWMA = latencyMS
		state.costEWMA = cost
		state.cacheAffinity = cacheHitRatio
	} else {
		state.latencyEWMA = alpha*latencyMS + (1-alpha)*state.latencyEWMA
		state.costEWMA = alpha*cost + (1-alpha)*state.costEWMA
		state.cacheAffinity = alpha*cacheHitRatio + (1-alpha)*state.cacheAffinity
	}
	state.last = m.clock.Now()
	m.mu.Unlock()
}

func (m *routingMetrics) stateLocked(id string) *deploymentStats {
	state := m.stats[id]
	if state == nil {
		state = &deploymentStats{}
		m.stats[id] = state
	}
	return state
}

// snapshot renders the current rolling window into the routing input. Stale
// windows are dropped so a deployment that went quiet falls back to defaults.
func (m *routingMetrics) snapshot(now time.Time) map[string]routing.DeploymentMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]routing.DeploymentMetrics{}
	for id, state := range m.stats {
		if now.Sub(state.last) > metricsWindow {
			delete(m.stats, id)
			continue
		}
		// Load is the normalized share of the window a deployment spent busy
		// (0 idle .. 1 saturated). It is a relative signal: soft scoring
		// normalizes again across candidates.
		total := state.success + state.failed
		load := 0.0
		if total > 0 {
			load = float64(state.failed) / float64(total)
		}
		if state.inflight > 0 {
			load = 1.0 // actively executing
		}
		result[id] = routing.DeploymentMetrics{
			LatencyMS:     state.latencyEWMA,
			Cost:          state.costEWMA,
			Load:          load,
			CacheAffinity: state.cacheAffinity,
		}
	}
	return result
}

// healthy reports whether a deployment's rolling window shows enough recent
// success to route to it. Deployments with no observations are healthy
// (unknown, not guilty).
func (m *routingMetrics) healthy(id string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.stats[id]
	if state == nil || now.Sub(state.last) > metricsWindow {
		return true
	}
	total := state.success + state.failed
	if total < 3 {
		// Too few samples: trust the probe/circuit, not the noise.
		return true
	}
	return float64(state.failed)/float64(total) <= 0.5
}

// hasData reports whether the deployment has a recent observation in the
// rolling window. Cold deployments (no data yet) fall back to probe-driven
// health instead of a confidence-based verdict.
func (m *routingMetrics) hasData(id string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.stats[id]
	if state == nil {
		return false
	}
	return now.Sub(state.last) <= metricsWindow
}
