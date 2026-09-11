package webkit

import (
	"errors"
	"log"
	"net/http"
)

// APIError is a structured HTTP error. Handlers return it to control the
// status code, machine-readable code, and extra body params; the engine's
// ErrorHandler renders it.
type APIError struct {
	Status int
	Code   string
	Params map[string]any
	Err    error
}

func (e *APIError) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Err.Error()
	}
	return e.Code
}

// Unwrap exposes the wrapped cause for errors.Is/As matching.
func (e *APIError) Unwrap() error { return e.Err }

// NewAPIError builds a structured error with optional body params.
func NewAPIError(status int, code string, params map[string]any) *APIError {
	return &APIError{Status: status, Code: code, Params: params}
}

// Wrap attaches the underlying cause (logged by the error handler on 5xx).
func (e *APIError) Wrap(err error) *APIError {
	e.Err = err
	return e
}

// ErrorBody is the canonical error payload shared by the default error
// handler and domain error handlers:
//
//	{"error": {"code": "...", "params": ...}}
func ErrorBody(code string, params map[string]any) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "params": params}}
}

// defaultErrorHandler renders APIErrors with their status/code/params and
// anything else as a 500 INTERNAL_ERROR.
func defaultErrorHandler(c *Context, err error) {
	var api *APIError
	if errors.As(err, &api) && api.Code != "" {
		if api.Status >= http.StatusInternalServerError && api.Err != nil {
			log.Printf("webkit: internal error: %v", api.Err)
		}
		_ = c.JSON(api.Status, ErrorBody(api.Code, api.Params))
		return
	}
	log.Printf("webkit: unhandled error: %v", err)
	_ = c.JSON(http.StatusInternalServerError, ErrorBody("INTERNAL_ERROR", nil))
}
