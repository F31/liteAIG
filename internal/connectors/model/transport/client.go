// Package transport builds provider HTTP clients with explicit timeout budgets.
package transport

import (
	"net"
	"net/http"
	"time"
)

type Timeouts struct{ Connect, TLSHandshake, ResponseHeader time.Duration }

func NewClient(timeouts Timeouts) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: timeouts.Connect, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: timeouts.TLSHandshake, ResponseHeaderTimeout: timeouts.ResponseHeader, ForceAttemptHTTP2: true}}
}
