package alert

import (
	"math"
	"time"
)

// AnomalyWindow is one aggregated usage window used for anomaly detection.
type AnomalyWindow struct {
	WindowStart time.Time
	WindowEnd   time.Time
	Cost        float64
}

// AnomalyResult reports an evidence-backed cost anomaly.
type AnomalyResult struct {
	Current      float64
	BaselineMean float64
	BaselineStd  float64
	Threshold    float64
	Detected     bool
	WindowStart  time.Time
	WindowEnd    time.Time
}

// DetectCostAnomaly compares the current window against a baseline using a
// z-score threshold (e.g. 2.0). Insufficient baseline windows yield no detection.
func DetectCostAnomaly(current AnomalyWindow, baseline []AnomalyWindow, zScore float64) AnomalyResult {
	if zScore <= 0 {
		zScore = 2.0
	}
	if len(baseline) < 2 {
		return AnomalyResult{Current: current.Cost, WindowStart: current.WindowStart, WindowEnd: current.WindowEnd}
	}
	mean, std := baselineStats(baseline)
	if std <= 0 {
		return AnomalyResult{Current: current.Cost, BaselineMean: mean, BaselineStd: std, Threshold: zScore, WindowStart: current.WindowStart, WindowEnd: current.WindowEnd}
	}
	score := (current.Cost - mean) / std
	return AnomalyResult{
		Current: current.Cost, BaselineMean: mean, BaselineStd: std, Threshold: zScore,
		Detected: score >= zScore, WindowStart: current.WindowStart, WindowEnd: current.WindowEnd,
	}
}

func baselineStats(windows []AnomalyWindow) (mean, std float64) {
	if len(windows) == 0 {
		return 0, 0
	}
	var sum float64
	for _, window := range windows {
		sum += window.Cost
	}
	mean = sum / float64(len(windows))
	var variance float64
	for _, window := range windows {
		d := window.Cost - mean
		variance += d * d
	}
	variance /= float64(len(windows))
	return mean, math.Sqrt(variance)
}
