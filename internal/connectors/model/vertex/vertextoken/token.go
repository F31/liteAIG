// Package vertextoken implements the Google service-account JWT → access-token
// exchange used to authenticate Vertex AI API calls. It uses only the standard
// library (crypto/rsa + encoding/pem for the private key, RS256 JWT signing).
package vertextoken

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ServiceAccount is the decoded service-account JSON credential.
type ServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
	ProjectID   string `json:"project_id"`
}

// ParseServiceAccount decodes a Google service-account JSON key.
func ParseServiceAccount(data []byte) (ServiceAccount, error) {
	var account ServiceAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return ServiceAccount{}, errors.New("Vertex credential must be a service-account JSON key")
	}
	if account.ClientEmail == "" || account.PrivateKey == "" {
		return ServiceAccount{}, errors.New("Vertex credential requires client_email and private_key")
	}
	if account.TokenURI == "" {
		account.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return account, nil
}

// Signer turns a service account into short-lived Google OAuth access tokens,
// caching them until they approach expiry.
type Signer struct {
	account ServiceAccount
	key     *rsa.PrivateKey
	client  *http.Client
	now     func() time.Time

	mu           sync.Mutex
	token        string
	accessExpiry time.Time
}

func New(account ServiceAccount, privateKey *rsa.PrivateKey, client *http.Client, now func() time.Time) *Signer {
	if now == nil {
		now = time.Now
	}
	return &Signer{account: account, key: privateKey, client: client, now: now}
}

// ParsePrivateKey reads an RSA private key from a PEM block (PKCS#1 or PKCS#8,
// including encrypted service-account keys marked "ENCRYPTED" — Vertex emits
// plain PKCS#8 RSA keys, which pem/x509 parse directly).
func ParsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no RSA private key PEM block")
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if key, ok := parsed.(*rsa.PrivateKey); ok {
			return key, nil
		}
		return nil, errors.New("service-account private key is not RSA")
	}
	if parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return parsed, nil
	}
	return nil, errors.New("failed to parse service-account RSA private key")
}

// AccessToken returns a cached, unexpired OAuth access token, minting one when
// needed.
func (s *Signer) AccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.token != "" && s.now().Before(s.accessExpiry.Add(-time.Minute)) {
		token := s.token
		s.mu.Unlock()
		return token, nil
	}
	s.mu.Unlock()
	return s.refresh(ctx)
}

func (s *Signer) refresh(ctx context.Context) (string, error) {
	assertion, err := s.assertion()
	if err != nil {
		return "", err
	}
	form := bytes.NewBufferString("grant_type=" + url("urn:ietf:params:oauth:grant-type:jwt-bearer") + "&assertion=" + url(assertion))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.account.TokenURI, form)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return "", errors.New("Vertex token exchange failed with status " + http.StatusText(response.StatusCode))
	}
	var wire struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(response.Body).Decode(&wire); err != nil {
		return "", err
	}
	if wire.AccessToken == "" {
		return "", errors.New("Vertex token exchange returned no access_token")
	}
	expiry := s.now().Add(time.Duration(wire.ExpiresIn) * time.Second)
	s.mu.Lock()
	s.token, s.accessExpiry = wire.AccessToken, expiry
	s.mu.Unlock()
	return wire.AccessToken, nil
}

// assertion builds a Google-signed JWT requesting an OAuth access token for
// the token exchange.
func (s *Signer) assertion() (string, error) {
	now := s.now().Unix()
	header := map[string]any{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":   s.account.ClientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   s.account.TokenURI,
		"iat":   now,
		"exp":   now + 3600,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func url(value string) string {
	return strings.NewReplacer("+", "%2B", "/", "%2F", "=", "%3D").Replace(value)
}
