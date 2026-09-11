// HTTP transport for signed RuntimeBundle delivery (Stage 11): the Control
// Plane serves each tenant's latest signed bundle from a Hub; Data Planes pull
// it, run Prepare (schema/checksum/signature), and atomically activate. Both
// sides share a bundle token so the endpoint is machine-to-machine only.
package bundle

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// TokenHeader is the request header carrying the shared bundle token.
const TokenHeader = "X-Bundle-Token"

// Hub holds the latest signed bundle per tenant on the Control Plane. It is
// populated by the config Service's BundlePublisher after every activation.
type Hub struct {
	mu      sync.RWMutex
	bundles map[string]wireBundle
}

// NewHub builds an empty bundle hub.
func NewHub() *Hub {
	return &Hub{bundles: map[string]wireBundle{}}
}

// Publish stores the latest signed bundle for a tenant under its ref.
func (h *Hub) Publish(snapshot *runtime.TenantRuntimeSnapshot, signed *RuntimeBundle) {
	if h == nil || snapshot == nil || signed == nil {
		return
	}
	wire := wireBundle{
		SchemaVersion: signed.SchemaVersion, TenantID: signed.TenantID, TenantRef: signed.TenantRef,
		ConfigVersion: signed.ConfigVersion, SecurityEpoch: signed.SecurityEpoch,
		SystemRuntimeVersion: signed.SystemRuntimeVersion, PublishedAt: signed.PublishedAt,
		PayloadChecksum: signed.PayloadChecksum, Signature: signed.Signature,
		SnapshotData: snapshot.Data(),
	}
	h.mu.Lock()
	h.bundles[snapshot.TenantRef] = wire
	h.mu.Unlock()
}

// Latest returns the latest signed bundle for a tenant ref.
func (h *Hub) Latest(tenantRef string) (*RuntimeBundle, bool) {
	if h == nil {
		return nil, false
	}
	h.mu.RLock()
	wire, ok := h.bundles[tenantRef]
	h.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return &RuntimeBundle{
		SchemaVersion: wire.SchemaVersion, TenantID: wire.TenantID, TenantRef: wire.TenantRef,
		ConfigVersion: wire.ConfigVersion, SecurityEpoch: wire.SecurityEpoch,
		SystemRuntimeVersion: wire.SystemRuntimeVersion, PublishedAt: wire.PublishedAt,
		PayloadChecksum: wire.PayloadChecksum, Signature: wire.Signature,
		Snapshot: runtime.NewTenantSnapshot(wire.SnapshotData),
	}, true
}

// TenantRefs lists the tenant refs with a published bundle.
func (h *Hub) TenantRefs() []string {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	refs := make([]string, 0, len(h.bundles))
	for ref := range h.bundles {
		refs = append(refs, ref)
	}
	return refs
}

// Handler serves signed bundles to Data Planes over HTTP.
type Handler struct {
	hub   *Hub
	token string
}

// NewHandler builds the control-plane bundle endpoint.
func NewHandler(hub *Hub, token string) *Handler {
	return &Handler{hub: hub, token: token}
}

// authorized validates the shared token with a constant-time compare.
func (h *Handler) authorized(r *http.Request) bool {
	if h.token == "" {
		return false
	}
	provided := r.Header.Get(TokenHeader)
	return subtle.ConstantTimeCompare([]byte(provided), []byte(h.token)) == 1
}

// ServeHTTP handles GET /api/runtime/bundles/{tenantRef} (or the list endpoint
// when no ref is present).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		http.Error(w, `{"code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	ref := r.PathValue("tenantRef")
	if ref == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{"tenantRefs": h.hub.TenantRefs()})
		return
	}
	bundle, ok := h.hub.Latest(ref)
	if !ok || bundle.Snapshot == nil {
		http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	data, err := bundle.Encode()
	if err != nil {
		http.Error(w, `{"code":"INTERNAL_ERROR"}`, http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(data)
}

var _ http.Handler = (*Handler)(nil)

// Client pulls signed bundles from a Control Plane endpoint.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient builds a bundle pull client.
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, token: token, http: httpClient}
}

// ListTenantRefs returns the tenant refs the Control Plane can serve.
func (c *Client) ListTenantRefs(ctx context.Context) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/runtime/bundles", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set(TokenHeader, c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New(statusError(response.StatusCode))
	}
	var out struct {
		TenantRefs []string `json:"tenantRefs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.TenantRefs, nil
}

// Fetch retrieves the latest signed bundle for a tenant ref.
func (c *Client) Fetch(ctx context.Context, tenantRef string) (*RuntimeBundle, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/runtime/bundles/"+tenantRef, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set(TokenHeader, c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New(statusError(response.StatusCode))
	}
	var wire wireBundle
	if err := json.NewDecoder(response.Body).Decode(&wire); err != nil {
		return nil, err
	}
	if wire.TenantRef == "" || wire.ConfigVersion == 0 {
		return nil, errors.New("control plane returned an incomplete bundle")
	}
	return &RuntimeBundle{
		SchemaVersion: wire.SchemaVersion, TenantID: wire.TenantID, TenantRef: wire.TenantRef,
		ConfigVersion: wire.ConfigVersion, SecurityEpoch: wire.SecurityEpoch,
		SystemRuntimeVersion: wire.SystemRuntimeVersion, PublishedAt: wire.PublishedAt,
		PayloadChecksum: wire.PayloadChecksum, Signature: wire.Signature,
		Snapshot: runtime.NewTenantSnapshot(wire.SnapshotData),
	}, nil
}

func statusError(code int) string {
	return fmt.Sprintf("control plane returned status %d (%s)", code, http.StatusText(code))
}
