package anthropic

type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
type ErrorResponse struct {
	Type  string      `json:"type"`
	Error ErrorDetail `json:"error"`
}

func NewErrorResponse(code, message string) ErrorResponse {
	return ErrorResponse{Type: "error", Error: ErrorDetail{Type: code, Message: message}}
}
