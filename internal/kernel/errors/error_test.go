package errors

import (
	stderrors "errors"
	"strings"
	"testing"
)

func TestErrorIncludesCorrelationAndUnwrapsCause(t *testing.T) {
	cause := stderrors.New("connection reset")
	err := &Error{
		Code:      "UPSTREAM_UNAVAILABLE",
		Message:   "upstream is unavailable",
		RequestID: "request-1",
		Retryable: true,
		Cause:     cause,
	}

	if !strings.Contains(err.Error(), "request_id=request-1") {
		t.Fatalf("Error() = %q", err.Error())
	}
	if !stderrors.Is(err, cause) {
		t.Fatal("Error does not unwrap its cause")
	}
}

func TestStatusForCode(t *testing.T) {
	cases := map[string]int{
		"UNAUTHORIZED":             401,
		"invalid_api_key":          401,
		"MODEL_FORBIDDEN":          403,
		"FEDERATION_DENIED":        403,
		"GUARDRAIL_BLOCKED":        400,
		"INVALID_REQUEST":          400,
		"REQUEST_TOO_LARGE":        413,
		"RATE_LIMITED":             429,
		"BUDGET_BLOCKED":           429,
		"MODEL_NOT_FOUND":          404,
		"REQUIRE_APPROVAL":         409,
		"A2A_TASK_IN_PROGRESS":     409,
		"A2A_TASK_LIMIT":           429,
		"CIRCUIT_OPEN":             503,
		"upstream_http_error":      502,
		"upstream_transport_error": 502,
		"UNKNOWN_NEW_CODE":         500,
	}
	for code, want := range cases {
		if got := StatusForCode(code); got != want {
			t.Errorf("StatusForCode(%q) = %d, want %d", code, got, want)
		}
	}
}
