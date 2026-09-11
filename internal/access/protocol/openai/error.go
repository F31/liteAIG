package openai

import "fmt"

// InvalidContentCode marks a malformed multimodal message content array so the
// request is rejected (400) instead of silently dropping text or image parts.
const InvalidContentCode = "INVALID_CONTENT"

// InvalidContentError reports a malformed message content array. The reason is
// structural only: client content (including inline image payloads and remote
// URLs) is never included in the error.
type InvalidContentError struct{ Reason string }

func (e *InvalidContentError) Error() string { return InvalidContentCode + ": " + e.Reason }

func invalidContentf(format string, args ...any) error {
	return &InvalidContentError{Reason: fmt.Sprintf(format, args...)}
}

type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code"`
}
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

func NewErrorResponse(code, message string) ErrorResponse {
	return ErrorResponse{Error: ErrorDetail{Message: message, Type: "liteaig_error", Code: code}}
}
