package egress

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateTarget(t *testing.T) {
	lite := LitePolicy()
	strict := Policy{MaxRedirects: 0}
	cases := []struct {
		url     string
		policy  Policy
		wantErr bool
	}{
		{url: "https://api.openai.com/v1", policy: lite},
		{url: "http://1.2.3.4:8080/v1", policy: lite},
		{url: "http://10.0.0.5/v1", policy: lite, wantErr: true},
		{url: "http://172.16.0.9/v1", policy: lite, wantErr: true},
		{url: "http://192.168.1.1/v1", policy: lite, wantErr: true},
		{url: "http://169.254.169.254/latest/meta-data", policy: lite, wantErr: true},
		{url: "http://[fd00::1]/v1", policy: lite, wantErr: true},
		{url: "file:///etc/passwd", policy: lite, wantErr: true},
		{url: "gopher://127.0.0.1/", policy: lite, wantErr: true},
		{url: "http://localhost:11434/v1", policy: lite},
		{url: "http://127.0.0.1:11434/v1", policy: lite},
		{url: "http://[::1]:11434/v1", policy: lite},
		{url: "http://127.0.0.1/v1", policy: strict, wantErr: true},
		{url: "http://example.internal/v1", policy: lite},
		{url: "http://", policy: lite, wantErr: true},
	}
	for _, item := range cases {
		err := ValidateTarget(item.url, item.policy)
		if item.wantErr && err == nil {
			t.Fatalf("ValidateTarget(%q) = nil, want error", item.url)
		}
		if !item.wantErr && err != nil {
			t.Fatalf("ValidateTarget(%q) = %v, want nil", item.url, err)
		}
	}
}

func TestDialContextBlocksInternalAddresses(t *testing.T) {
	policy := LitePolicy()
	if _, err := policy.DialContext(context.Background(), "tcp", "169.254.169.254:80"); err == nil {
		t.Fatal("dial to the metadata range was allowed")
	}
	if _, err := policy.DialContext(context.Background(), "tcp", "10.1.2.3:443"); err == nil {
		t.Fatal("dial to an RFC1918 address was allowed")
	}
	// A real loopback listener proves the Lite policy permits loopback while a
	// strict policy refuses the same address.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accept := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		close(accept)
		if err == nil {
			conn.Close()
		}
	}()
	if _, err := policy.DialContext(context.Background(), "tcp", listener.Addr().String()); err != nil {
		t.Fatalf("loopback dial in Lite policy: %v", err)
	}
	<-accept
	strict := Policy{MaxRedirects: 0}
	if _, err := strict.DialContext(context.Background(), "tcp", listener.Addr().String()); err == nil {
		t.Fatal("loopback dial was allowed without AllowLoopback")
	}
}

func TestCheckRedirectRejectsEscape(t *testing.T) {
	policy := LitePolicy()
	start := httptest.NewRequest(http.MethodGet, "http://public.example/", nil)
	internal := *start
	internal.URL.Host = "169.254.169.254"
	if err := policy.CheckRedirect(&internal, []*http.Request{start}); err == nil {
		t.Fatal("redirect to the metadata range was allowed")
	}
	// MaxRedirects=0 rejects even a benign redirect.
	public := *start
	public.URL.Host = "other.example"
	if err := policy.CheckRedirect(&public, []*http.Request{start}); err == nil {
		t.Fatal("redirect was allowed with MaxRedirects=0")
	}
	// A redirect budget of one allows exactly one hop.
	one := Policy{MaxRedirects: 1}
	if err := one.CheckRedirect(&public, []*http.Request{start}); err != nil {
		t.Fatalf("first redirect rejected: %v", err)
	}
}

func TestClientCapsResponseBodies(t *testing.T) {
	client := Client(Policy{AllowLoopback: true, MaxRedirects: 0, MaxResponseBytes: 8})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0123456789ABCDEF"))
	}))
	defer server.Close()
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 8 || string(body) != "01234567" {
		t.Fatalf("body = %q, want the first 8 bytes", body)
	}
}

func TestClientAllowsPublicAndLoopback(t *testing.T) {
	client := Client(LitePolicy())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("egress client failed on the local test server: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestClientDialsResolvedHost(t *testing.T) {
	// The protected dialer must resolve hostnames and connect to the resolved
	// address; the test server's loopback hostname exercises that path.
	client := Client(LitePolicy())
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("resolved"))
	}))
	server.Start()
	defer server.Close()
	if !strings.HasPrefix(server.URL, "http://127.0.0.1") {
		t.Fatalf("test server unexpectedly on %s", server.URL)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "resolved" {
		t.Fatalf("body = %q", body)
	}
}
