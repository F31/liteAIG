package adminapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	identityoidc "github.com/F31/liteAIG/internal/identity/oidc"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

// OIDCLoginConfig contains public-client settings for authorization-code PKCE.
type OIDCLoginConfig struct {
	AuthorizationEndpoint string
	ClientID              string
	Audience              string
	RedirectURI           string
	StateTTL              time.Duration
	Roles                 identityoidc.RoleClaims
}

// OIDCTokenExchanger exchanges an authorization code without exposing tokens
// to the Console.
type OIDCTokenExchanger interface {
	Exchange(context.Context, string, string, string) (string, error)
}

type oidcState struct {
	verifier string
	expires  time.Time
}

// OIDCLogin owns the short-lived PKCE state and establishes admin sessions.
type OIDCLogin struct {
	Sessions *SessionManager
	Verifier identityoidc.Verifier
	Exchange OIDCTokenExchanger
	Config   OIDCLoginConfig
	// ResolveTenant returns the tenant scope a freshly signed-in OIDC session
	// belongs to. Lite is single-tenant and its bootstrap tenant ID is random
	// (never "default"), so the composition root injects a resolver that
	// returns the actual tenant; nil keeps the claim-derived tenant.
	ResolveTenant func(ctx context.Context) (string, error)
	// Audit records authentication events (may be nil).
	Audit  func(ctx context.Context, actor, action, resourceID string) error
	random io.Reader
	now    func() time.Time
	mu     sync.Mutex
	states map[string]oidcState
}

// NewOIDCLogin validates and constructs the optional OIDC login path.
func NewOIDCLogin(sessions *SessionManager, verifier identityoidc.Verifier, exchanger OIDCTokenExchanger, config OIDCLoginConfig) (*OIDCLogin, error) {
	if sessions == nil || verifier == nil || exchanger == nil || config.AuthorizationEndpoint == "" || config.ClientID == "" || config.Audience == "" || config.RedirectURI == "" {
		return nil, errors.New("complete OIDC login configuration is required")
	}
	if _, err := url.ParseRequestURI(config.AuthorizationEndpoint); err != nil {
		return nil, errors.New("invalid OIDC authorization endpoint")
	}
	if config.StateTTL <= 0 {
		config.StateTTL = 5 * time.Minute
	}
	return &OIDCLogin{Sessions: sessions, Verifier: verifier, Exchange: exchanger, Config: config, random: rand.Reader, now: time.Now, states: map[string]oidcState{}}, nil
}

func (o *OIDCLogin) register(engine *webkit.Engine) {
	engine.Handle("GET /api/admin/oidc/config", o.configuration)
	engine.Handle("POST /api/admin/oidc/start", o.start)
	engine.Handle("POST /api/admin/oidc/session", o.session)
}

func (o *OIDCLogin) configuration(c *webkit.Context) error {
	return c.JSON(http.StatusOK, map[string]bool{"enabled": true})
}

func (o *OIDCLogin) start(c *webkit.Context) error {
	state, err := randomToken(o.random)
	if err != nil {
		return internalError()
	}
	verifier, err := randomToken(o.random)
	if err != nil {
		return internalError()
	}
	now := o.now()
	o.mu.Lock()
	for key, entry := range o.states {
		if now.After(entry.expires) {
			delete(o.states, key)
		}
	}
	o.states[state] = oidcState{verifier: verifier, expires: now.Add(o.Config.StateTTL)}
	o.mu.Unlock()

	destination, _ := url.Parse(o.Config.AuthorizationEndpoint)
	query := destination.Query()
	query.Set("response_type", "code")
	query.Set("client_id", o.Config.ClientID)
	query.Set("redirect_uri", o.Config.RedirectURI)
	query.Set("scope", "openid profile email")
	query.Set("state", state)
	challenge := sha256.Sum256([]byte(verifier))
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	destination.RawQuery = query.Encode()
	return c.JSON(http.StatusOK, map[string]string{"authorizationUrl": destination.String()})
}

func (o *OIDCLogin) session(c *webkit.Context) error {
	r := c.Request()
	var input struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := c.Bind(&input, 4096); err != nil || input.Code == "" || input.State == "" {
		return invalidRequest()
	}
	verifier, ok := o.consume(input.State)
	if !ok {
		return webkit.NewAPIError(http.StatusUnauthorized, "OIDC_FLOW_INVALID", nil)
	}
	idToken, err := o.Exchange.Exchange(r.Context(), input.Code, verifier, o.Config.RedirectURI)
	if err != nil {
		return webkit.NewAPIError(http.StatusUnauthorized, "OIDC_EXCHANGE_FAILED", nil)
	}
	claims, err := o.Verifier.VerifyIDToken(r.Context(), idToken, o.Config.Audience)
	if err != nil || claims == nil || claims.Sub == "" {
		return webkit.NewAPIError(http.StatusUnauthorized, "OIDC_TOKEN_INVALID", nil)
	}
	principal := identityoidc.MapToPrincipal(claims, o.Config.Roles)
	tenantID := principal.TenantID
	if o.ResolveTenant != nil {
		if resolved, err := o.ResolveTenant(r.Context()); err == nil && resolved != "" {
			tenantID = resolved
		}
	}
	csrf, err := o.Sessions.Create(c.Response(), Session{AdminID: principal.ExternalSubject, TenantID: tenantID, Role: principal.Role, AuthMethod: "oidc"})
	if err != nil {
		return internalError()
	}
	if o.Audit != nil {
		if auditErr := o.Audit(c.Request().Context(), principal.ExternalSubject, "auth.oidc_login", claims.Sub); auditErr != nil {
			return internalError()
		}
	}
	return c.JSON(http.StatusOK, map[string]string{"csrfToken": csrf})
}

func (o *OIDCLogin) consume(state string) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	entry, ok := o.states[state]
	delete(o.states, state)
	if !ok || o.now().After(entry.expires) {
		return "", false
	}
	return entry.verifier, true
}

func randomToken(source io.Reader) (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(source, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// HTTPTokenExchanger exchanges a code at the configured token endpoint.
type HTTPTokenExchanger struct {
	Client        *http.Client
	TokenEndpoint string
	ClientID      string
}

func (e HTTPTokenExchanger) Exchange(ctx context.Context, code, verifier, redirectURI string) (string, error) {
	if e.TokenEndpoint == "" || e.ClientID == "" {
		return "", errors.New("OIDC token exchange is not configured")
	}
	client := e.Client
	if client == nil {
		client = http.DefaultClient
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {e.ClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", errors.New("OIDC token exchange failed")
	}
	var output struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&output); err != nil || output.IDToken == "" {
		return "", errors.New("OIDC token response is invalid")
	}
	return output.IDToken, nil
}
