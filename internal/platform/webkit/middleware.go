package webkit

import (
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
)

// Recover converts a handler panic into a 500 response while the reply has
// not started. Once bytes are on the wire (mid-stream panics), the panic is
// logged and re-raised so net/http terminates the request, matching the
// previous per-request recovery behavior.
func Recover() Middleware {
	return func(next Handler) Handler {
		return func(c *Context) (err error) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if c.Written() {
					log.Printf("webkit: panic after response started: %v\n%s", recovered, debug.Stack())
					panic(recovered)
				}
				log.Printf("webkit: recovered handler panic: %v\n%s", recovered, debug.Stack())
				err = NewAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", nil).Wrap(fmt.Errorf("panic: %v", recovered))
			}()
			return next(c)
		}
	}
}

// SecurityHeaders adds the defensive response header baseline (no-store,
// nosniff, referrer, permissions, COOP) plus any extra name/value pairs a
// domain wants (e.g. a strict CSP for the admin console plane).
func SecurityHeaders(extra ...[2]string) Middleware {
	return func(next Handler) Handler {
		return func(c *Context) error {
			h := c.Header()
			h.Set("Cache-Control", "no-store")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			for _, kv := range extra {
				h.Set(kv[0], kv[1])
			}
			return next(c)
		}
	}
}
