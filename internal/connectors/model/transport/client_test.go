package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResponseHeaderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { time.Sleep(50 * time.Millisecond); w.WriteHeader(200) }))
	defer server.Close()
	client := NewClient(Timeouts{Connect: time.Second, TLSHandshake: time.Second, ResponseHeader: 10 * time.Millisecond})
	if _, err := client.Get(server.URL); err == nil {
		t.Fatal("request exceeded response-header timeout without failing")
	}
}
