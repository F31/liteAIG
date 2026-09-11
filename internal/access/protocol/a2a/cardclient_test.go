package a2a

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/egress"
)

const validCardBody = `{"name":"partner","url":"https://partner.example/agent","description":"d","version":"1.0.0","skills":["invoice.read"],"capabilities":["invoice.read"]}`

// discoveryServer serves a configurable well-known card. counters let tests
// observe how many requests arrived and whether they were conditional.
type discoveryServer struct {
	handler       atomic.Int64 // total well-known hits
	revalidations atomic.Int64 // hits carrying If-None-Match
	cardBody      string
	etag          string
	status        int
	contentType   string
}

func newDiscoveryServer() *discoveryServer {
	return &discoveryServer{status: http.StatusOK, contentType: "application/json"}
}

func (s *discoveryServer) serve(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handler.Add(1)
		if inm := r.Header.Get("If-None-Match"); inm != "" {
			s.revalidations.Add(1)
			if s.etag != "" && inm == s.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if r.URL.Path != "/.well-known/agent-card.json" {
			http.NotFound(w, r)
			return
		}
		if s.status == http.StatusNotModified {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if s.etag != "" {
			w.Header().Set("ETag", s.etag)
		}
		w.Header().Set("Content-Type", s.contentType)
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, s.cardBody)
	}))
}

func newTestDiscoveryClient(store CardStore) *Client {
	return NewClient(egress.Client(egress.LitePolicy()), store)
}

func TestClientFetch200Then304ReturnsCachedWithoutReparse(t *testing.T) {
	server := newDiscoveryServer()
	server.cardBody = validCardBody
	server.etag = `"v1"`
	ts := server.serve(t)
	defer ts.Close()

	store := NewMemoryCardStore(5 * time.Minute)
	client := newTestDiscoveryClient(store)
	ctx := context.Background()

	first, err := client.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if first.Name != "partner" || first.Version != "1.0.0" {
		t.Fatalf("first card = %+v", first)
	}
	// Corrupt the served body: a 304 must never parse it, so this would fail
	// if the client re-requested the body or re-parsed anything.
	server.cardBody = "{definitely not json"

	second, err := client.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("revalidated fetch: %v", err)
	}
	if second.Name != "partner" || second.Version != "1.0.0" {
		t.Fatalf("cached card lost after 304: %+v", second)
	}
	if got := server.handler.Load(); got != 2 {
		t.Fatalf("handler hits = %d, want 2 (200 + 304)", got)
	}
	if got := server.revalidations.Load(); got != 1 {
		t.Fatalf("conditional hits = %d, want 1", got)
	}
	// The returned card is a clone: mutating it must not corrupt the cache.
	second.Name = "mutated"
	again, err := client.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("fetch after mutation: %v", err)
	}
	if again.Name != "partner" {
		t.Fatalf("cache was corrupted by caller mutation: %q", again.Name)
	}
}

func TestClientFetchMalformedCardNotCachedAndRepopulates(t *testing.T) {
	server := newDiscoveryServer()
	ts := server.serve(t)
	defer ts.Close()

	store := NewMemoryCardStore(5 * time.Minute)
	client := newTestDiscoveryClient(store)

	server.cardBody = `{"name":"broken"}` // missing url + version
	if _, err := client.Fetch(context.Background(), ts.URL); !errors.Is(err, ErrAgentCardInvalid) {
		t.Fatalf("malformed card err = %v, want ErrAgentCardInvalid", err)
	}
	if _, ok := store.Get(ts.URL); ok {
		t.Fatal("malformed card was cached")
	}
	server.cardBody = validCardBody
	good, err := client.Fetch(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("good fetch after malformed: %v", err)
	}
	if good.Name != "partner" {
		t.Fatalf("unexpected good card %+v", good)
	}
	if cached, ok := store.Get(ts.URL); !ok || cached.Card.Name != "partner" {
		t.Fatalf("good card not cached: %+v ok=%v", cached, ok)
	}
}

func TestClientFetchOversizedBodyRejected(t *testing.T) {
	server := newDiscoveryServer()
	// Description far over the 256 KiB discovery cap.
	server.cardBody = `{"name":"big","url":"https://big.example","version":"1.0","description":"` + strings.Repeat("x", (maxAgentCardBytes)+16) + `"}`
	ts := server.serve(t)
	defer ts.Close()

	store := NewMemoryCardStore(5 * time.Minute)
	client := newTestDiscoveryClient(store)
	_, err := client.Fetch(context.Background(), ts.URL)
	if !errors.Is(err, ErrAgentCardTooLarge) {
		t.Fatalf("oversized body err = %v, want ErrAgentCardTooLarge", err)
	}
	if _, ok := store.Get(ts.URL); ok {
		t.Fatal("oversized response was cached")
	}
}

func TestClientFetchRejectsNonJSONContentType(t *testing.T) {
	server := newDiscoveryServer()
	server.cardBody = validCardBody
	server.contentType = "text/plain"
	ts := server.serve(t)
	defer ts.Close()

	client := newTestDiscoveryClient(NewMemoryCardStore(time.Minute))
	if _, err := client.Fetch(context.Background(), ts.URL); !errors.Is(err, ErrAgentCardContentType) {
		t.Fatalf("content-type err = %v, want ErrAgentCardContentType", err)
	}
}

func TestClientFetchTTLExpiryTriggersRefetch(t *testing.T) {
	server := newDiscoveryServer()
	server.cardBody = validCardBody
	server.etag = `"v2"`
	ts := server.serve(t)
	defer ts.Close()

	store := NewMemoryCardStore(40 * time.Millisecond)
	client := newTestDiscoveryClient(store)
	ctx := context.Background()

	if _, err := client.Fetch(ctx, ts.URL); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	// Immediately (fresh) the second fetch is conditional and returns 304.
	if _, err := client.Fetch(ctx, ts.URL); err != nil {
		t.Fatalf("fresh revalidate: %v", err)
	}
	hitsAfter304 := server.handler.Load()

	time.Sleep(60 * time.Millisecond) // outlive the store TTL
	third, err := client.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("fetch after TTL expiry: %v", err)
	}
	if third.Name != "partner" {
		t.Fatalf("refetched card = %+v", third)
	}
	if got := server.handler.Load(); got != hitsAfter304+1 {
		t.Fatalf("handler hits after expiry = %d, want %d (full refetch)", got, hitsAfter304+1)
	}
	if got := server.revalidations.Load(); got != 1 {
		t.Fatalf("conditional hits = %d, want 1 (expired fetch must not be conditional)", got)
	}
}

func TestClientFetchRejectsInvalidTargetsWithoutRequest(t *testing.T) {
	server := newDiscoveryServer()
	ts := server.serve(t)
	defer ts.Close()

	client := newTestDiscoveryClient(NewMemoryCardStore(time.Minute))
	cases := []string{
		"",
		"partner.example",                 // no scheme
		"ftp://partner.example",           // bad scheme
		"http://10.1.2.3/.well-known",     // internal address, not loopback
		"http://[fe80::1]/",               // link-local
		"http://",                         // no host
		"://missing-scheme.example/path?", // malformed
	}
	for _, base := range cases {
		if _, err := client.Fetch(context.Background(), base); err == nil {
			t.Fatalf("base %q was accepted", base)
		}
	}
	if got := server.handler.Load(); got != 0 {
		t.Fatalf("handler hits = %d, want 0 for rejected targets", got)
	}
}

func TestClientFetchValidatesDiscoveredCardURL(t *testing.T) {
	server := newDiscoveryServer()
	// Card URL must be an absolute http(s) URL even when the fetch target is fine.
	server.cardBody = `{"name":"x","url":"partner.example/path","version":"1.0"}`
	ts := server.serve(t)
	defer ts.Close()

	client := newTestDiscoveryClient(NewMemoryCardStore(time.Minute))
	if _, err := client.Fetch(context.Background(), ts.URL); !errors.Is(err, ErrAgentCardInvalid) {
		t.Fatalf("relative card url err = %v, want ErrAgentCardInvalid", err)
	}
}

func TestClientFetchUnexpectedStatusIsError(t *testing.T) {
	server := newDiscoveryServer()
	server.status = http.StatusInternalServerError
	ts := server.serve(t)
	defer ts.Close()

	client := newTestDiscoveryClient(NewMemoryCardStore(time.Minute))
	if _, err := client.Fetch(context.Background(), ts.URL); !errors.Is(err, ErrAgentCardFetch) {
		t.Fatalf("500 err = %v, want ErrAgentCardFetch", err)
	}
}

func TestClientNilStoreFetchesWithoutCaching(t *testing.T) {
	server := newDiscoveryServer()
	server.cardBody = validCardBody
	ts := server.serve(t)
	defer ts.Close()

	client := newTestDiscoveryClient(nil)
	card, err := client.Fetch(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("fetch with nil store: %v", err)
	}
	if card.Name != "partner" {
		t.Fatalf("unexpected card %+v", card)
	}
}
