package adminapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"io"
	"net/http"
	"sync"
	"time"
)

type SessionConfig struct {
	CookieName string
	TTL        time.Duration
	Secure     bool
}
type storedSession struct {
	Session   Session
	CSRFHash  [32]byte
	CSRFToken string
	ExpiresAt time.Time
}
type SessionManager struct {
	config   SessionConfig
	clock    contracts.Clock
	random   io.Reader
	mu       sync.Mutex
	sessions map[[32]byte]storedSession
}

func NewSessionManager(config SessionConfig, clock contracts.Clock, random io.Reader) (*SessionManager, error) {
	if config.CookieName == "" || config.TTL <= 0 || !config.Secure || clock == nil {
		return nil, errors.New("secure session configuration is required")
	}
	if random == nil {
		random = rand.Reader
	}
	return &SessionManager{config: config, clock: clock, random: random, sessions: make(map[[32]byte]storedSession)}, nil
}
func (m *SessionManager) Create(w http.ResponseWriter, session Session) (string, error) {
	token, err := m.token()
	if err != nil {
		return "", err
	}
	csrf, err := m.token()
	if err != nil {
		return "", err
	}
	tokenHash := sha256.Sum256([]byte(token))
	csrfHash := sha256.Sum256([]byte(csrf))
	expires := m.clock.Now().Add(m.config.TTL)
	m.mu.Lock()
	m.sessions[tokenHash] = storedSession{Session: session, CSRFHash: csrfHash, CSRFToken: csrf, ExpiresAt: expires}
	m.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: m.config.CookieName, Value: token, Path: "/", Expires: expires, MaxAge: int(m.config.TTL.Seconds()), Secure: m.config.Secure, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	return csrf, nil
}
func (m *SessionManager) Authorize(r *http.Request) (Session, error) {
	cookie, err := r.Cookie(m.config.CookieName)
	if err != nil {
		return Session{}, errors.New("missing session")
	}
	tokenHash := sha256.Sum256([]byte(cookie.Value))
	m.mu.Lock()
	stored, ok := m.sessions[tokenHash]
	if ok && m.clock.Now().After(stored.ExpiresAt) {
		delete(m.sessions, tokenHash)
		ok = false
	}
	m.mu.Unlock()
	if !ok {
		return Session{}, errors.New("invalid session")
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		csrfHash := sha256.Sum256([]byte(r.Header.Get("X-CSRF-Token")))
		if subtle.ConstantTimeCompare(csrfHash[:], stored.CSRFHash[:]) != 1 {
			return Session{}, errors.New("invalid CSRF token")
		}
	}
	return stored.Session, nil
}

// CSRFTokenFor returns the CSRF token bound to the current session cookie.
// Used to restore the token after an SPA full reload (me surface).
func (m *SessionManager) CSRFTokenFor(r *http.Request) (string, error) {
	cookie, err := r.Cookie(m.config.CookieName)
	if err != nil {
		return "", errors.New("missing session")
	}
	tokenHash := sha256.Sum256([]byte(cookie.Value))
	m.mu.Lock()
	stored, ok := m.sessions[tokenHash]
	if ok && m.clock.Now().After(stored.ExpiresAt) {
		delete(m.sessions, tokenHash)
		ok = false
	}
	m.mu.Unlock()
	if !ok {
		return "", errors.New("invalid session")
	}
	return stored.CSRFToken, nil
}

func (m *SessionManager) SwitchTenant(r *http.Request, tenantID string) (Session, error) {
	cookie, err := r.Cookie(m.config.CookieName)
	if err != nil {
		return Session{}, errors.New("missing session")
	}
	tokenHash := sha256.Sum256([]byte(cookie.Value))
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.sessions[tokenHash]
	if ok && m.clock.Now().After(stored.ExpiresAt) {
		delete(m.sessions, tokenHash)
		ok = false
	}
	if !ok {
		return Session{}, errors.New("invalid session")
	}
	stored.Session.TenantID = tenantID
	m.sessions[tokenHash] = stored
	return stored.Session, nil
}

func (m *SessionManager) Destroy(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(m.config.CookieName); err == nil {
		hash := sha256.Sum256([]byte(cookie.Value))
		m.mu.Lock()
		delete(m.sessions, hash)
		m.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: m.config.CookieName, Path: "/", MaxAge: -1, Secure: m.config.Secure, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// InvalidateForAdmin destroys every live session of one account. A password
// reset (email or emergency) must void any sessions the old password could
// still present.
func (m *SessionManager) InvalidateForAdmin(adminID string) {
	if adminID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for hash, stored := range m.sessions {
		if stored.Session.AdminID == adminID {
			delete(m.sessions, hash)
		}
	}
}
func (m *SessionManager) token() (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(m.random, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

var _ Authorizer = (*SessionManager)(nil)
