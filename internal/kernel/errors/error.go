// Package errors defines stable client-safe Kernel errors.
package errors

import (
	"fmt"
	"net/http"
)

// Error is a client-safe failure with correlation and retry guidance.
type Error struct {
	Code      string
	Message   string
	RequestID string
	Retryable bool
	Cause     error
}

func (e *Error) Error() string {
	if e.RequestID == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s (request_id=%s)", e.Code, e.Message, e.RequestID)
}

// Unwrap exposes the internal cause to server-side error inspection.
func (e *Error) Unwrap() error { return e.Cause }

// StatusForCode is the single source of truth mapping a stable error code to
// its HTTP status. Both the admin and the gateway renderers must use it so a
// new code can never drift across the two surfaces.
func StatusForCode(code string) int {
	switch code {
	case "UNAUTHORIZED", "INVALID_API_KEY", "invalid_api_key":
		return http.StatusUnauthorized
	case "MODEL_FORBIDDEN", "TOOL_FORBIDDEN", "PERMISSION_DENIED", "FEDERATION_UNTRUSTED", "FEDERATION_DENIED", "FEDERATION_HOP_LIMIT":
		return http.StatusForbidden
	case "INVALID_REQUEST", "TOOL_SCHEMA_INVALID", "GUARDRAIL_BLOCKED", "OUTPUT_GUARDRAIL_BLOCKED", "TOOL_DLP_BLOCKED":
		return http.StatusBadRequest
	case "REQUEST_TOO_LARGE":
		return http.StatusRequestEntityTooLarge
	case "RATE_LIMITED", "BUDGET_EXCEEDED", "BUDGET_BLOCKED", "A2A_TASK_LIMIT":
		return http.StatusTooManyRequests
	case "TOOL_NOT_FOUND", "MODEL_NOT_FOUND", "NOT_FOUND":
		return http.StatusNotFound
	case "REQUIRE_APPROVAL", "A2A_TASK_IN_PROGRESS":
		return http.StatusConflict
	case "NO_ELIGIBLE_DEPLOYMENT", "CIRCUIT_OPEN", "STREAMING_UNSUPPORTED", "LEASE_BUSY", "DRAINING":
		return http.StatusServiceUnavailable
	case "upstream_http_error", "upstream_transport_error":
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
