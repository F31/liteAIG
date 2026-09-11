package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/platform/egress"
)

// Discovery constants for the A2A well-known Agent Card resource.
const (
	agentCardPath = "/.well-known/agent-card.json"

	// maxAgentCardBytes caps a discovery response body well below the egress
	// transport cap so a misbehaving peer cannot force a large allocation.
	maxAgentCardBytes = 256 << 10

	// defaultCardCacheTTL bounds how long a fresh card is cached when the
	// response carries no cache signal; entries are still revalidated by ETag
	// on the next Fetch.
	defaultCardCacheTTL = 5 * time.Minute

	// discoveryTimeout bounds a single well-known fetch. Callers may impose a
	// shorter deadline through the context.
	discoveryTimeout = 10 * time.Second
)

// Discovery errors are generic on purpose: they never embed the card URL,
// payload, or any response body so they are safe to surface in logs. A fetched
// card is a candidate only; verification (VerifyCardSignature) and lifecycle
// activation are separate steps that never happen inside this client.
var (
	// ErrAgentCardFetch is the base failure for a discovery HTTP round trip.
	ErrAgentCardFetch = errors.New("agent card fetch failed")
	// ErrAgentCardTooLarge reports a response body over the discovery cap.
	ErrAgentCardTooLarge = errors.New("agent card response too large")
	// ErrAgentCardContentType reports a non-JSON well-known response.
	ErrAgentCardContentType = errors.New("agent card response is not JSON")
	// ErrAgentCardInvalid reports a card that fails discovery validation.
	ErrAgentCardInvalid = errors.New("invalid agent card")
)

// Client discovers A2A Agent Cards from their well-known URL with a bounded
// cache. The client never trusts a card: a successful Fetch returns a parsed
// candidate for the caller to verify and activate through the lifecycle.
type Client struct {
	httpClient *http.Client
	store      CardStore
}

// NewClient builds a discovery Client. client carries the egress posture
// (egress.Client(egress.LitePolicy()) for outbound discovery) and store backs
// the per-base URL cache; both may be nil, in which case the client uses the
// default transport and a non-caching behavior.
func NewClient(client *http.Client, store CardStore) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{httpClient: client, store: store}
}

// Fetch retrieves and validates the Agent Card published at
// baseURL/.well-known/agent-card.json. The baseURL is validated against the
// Lite egress policy before any request is made. A 304 Not Modified returns
// the cached card without re-parsing the body; a 200 repopulates the cache
// under the response ETag. Malformed or oversized responses are never cached,
// so a transient bad publish cannot poison a previously good card.
func (c *Client) Fetch(ctx context.Context, baseURL string) (*AgentCard, error) {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("%w: empty target", ErrAgentCardFetch)
	}
	if err := egress.ValidateTarget(base, egress.LitePolicy()); err != nil {
		return nil, err
	}
	endpoint := base + agentCardPath

	var cached *cachedCard
	key := base
	if c.store != nil {
		cached, _ = c.store.Get(key)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentCardFetch, err)
	}
	// Conditional GET: only send the validator when the cache still holds a
	// fresh entry. An expired entry therefore triggers a full 200 refetch.
	if cached != nil && cached.ETag != "" {
		request.Header.Set("If-None-Match", cached.ETag)
	}
	fetchCtx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	response, err := c.httpClient.Do(request.WithContext(fetchCtx))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentCardFetch, err)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusNotModified:
		if cached == nil || cached.Card == nil {
			return nil, fmt.Errorf("%w: unsolicited 304", ErrAgentCardFetch)
		}
		// Revalidation success slides the freshness window so steady-state
		// polling does not re-download an unchanged card.
		if c.store != nil {
			c.store.Put(key, cached.Card, cached.ETag, time.Now().Add(defaultCardCacheTTL))
		}
		return cached.Card, nil
	case http.StatusOK:
		// Fall through to parse below.
	default:
		return nil, fmt.Errorf("%w: unexpected status %d", ErrAgentCardFetch, response.StatusCode)
	}

	if !isJSONContentType(response.Header.Get("Content-Type")) {
		return nil, ErrAgentCardContentType
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAgentCardBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentCardFetch, err)
	}
	if int64(len(body)) > maxAgentCardBytes {
		return nil, ErrAgentCardTooLarge
	}
	var card AgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentCardInvalid, err)
	}
	if err := validateDiscoveredCard(&card); err != nil {
		return nil, err
	}
	if c.store != nil {
		c.store.Put(key, &card, response.Header.Get("ETag"), time.Now().Add(defaultCardCacheTTL))
	}
	return &card, nil
}

// validateDiscoveredCard is the stricter discovery-time validation: name, URL,
// and version are required and the URL must be an absolute http(s) URL. The
// published card is operator-authored content; discovery only sanity-checks it
// and never treats it as trusted (verification + lifecycle activation follow).
func validateDiscoveredCard(card *AgentCard) error {
	if card.Name == "" || card.URL == "" || card.Version == "" {
		return fmt.Errorf("%w: name, url, and version are required", ErrAgentCardInvalid)
	}
	parsed, err := url.Parse(card.URL)
	if err != nil {
		return fmt.Errorf("%w: malformed url", ErrAgentCardInvalid)
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: url must be an absolute http(s) url", ErrAgentCardInvalid)
	}
	return nil
}

// isJSONContentType reports whether a Content-Type is application/json or a
// structured JSON variant.
func isJSONContentType(value string) bool {
	if value == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	if mediaType == "application/json" {
		return true
	}
	return strings.HasSuffix(mediaType, "+json")
}
