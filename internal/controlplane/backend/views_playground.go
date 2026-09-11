package backend

type PlaygroundRequest struct {
	Model, Input string
	Stream       bool
}

type PlaygroundResponse struct {
	RequestID          string   `json:"requestId"`
	Output             string   `json:"output"`
	SelectedDeployment string   `json:"selectedDeployment"`
	InputTokens        int64    `json:"inputTokens"`
	OutputTokens       int64    `json:"outputTokens"`
	Cost               *float64 `json:"cost,omitempty"`
	LatencyMS          int64    `json:"latencyMS"`
}
