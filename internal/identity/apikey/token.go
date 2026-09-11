// Package apikey owns Virtual Key generation, storage facts, and verification.
package apikey

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

const Prefix = "sk-lia-v1"

// New token layout (compact): tenantRef 16B hex + publicID 8B hex + secret 24B hex.
// Legacy tokens (publicID 16B + secret 32B) remain valid for verification.
const (
	tenantRefBytes = 16
	publicIDBytes  = 8
	secretBytes    = 24
	legacyPublicID = 16
	legacySecret   = 32
)

type Token struct{ TenantRef, PublicID, Secret string }

func (t Token) String() string { return Prefix + "_" + t.TenantRef + "_" + t.PublicID + "_" + t.Secret }

type Generator struct{ random io.Reader }

func NewGenerator(random io.Reader) *Generator {
	if random == nil {
		random = rand.Reader
	}
	return &Generator{random: random}
}

func (g *Generator) NewTenantRef() (string, error) { return g.randomHex(16) }

func (g *Generator) NewKey(tenantRef string) (Token, error) {
	if !validHex(tenantRef, tenantRefBytes) {
		return Token{}, errors.New("invalid tenant_ref")
	}
	publicID, err := g.randomHex(publicIDBytes)
	if err != nil {
		return Token{}, err
	}
	secret, err := g.randomHex(secretBytes)
	if err != nil {
		return Token{}, err
	}
	return Token{TenantRef: tenantRef, PublicID: publicID, Secret: secret}, nil
}

func Parse(value string) (Token, error) {
	parts := strings.Split(value, "_")
	if len(parts) != 4 || parts[0] != Prefix || !validHex(parts[1], tenantRefBytes) {
		return Token{}, errors.New("invalid virtual key")
	}
	if !validHex(parts[2], publicIDBytes) || !validHex(parts[3], secretBytes) {
		if !validHex(parts[2], legacyPublicID) || !validHex(parts[3], legacySecret) {
			return Token{}, errors.New("invalid virtual key")
		}
	}
	return Token{TenantRef: parts[1], PublicID: parts[2], Secret: parts[3]}, nil
}

func (g *Generator) randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(g.random, value); err != nil {
		return "", fmt.Errorf("generate virtual key entropy: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func validHex(value string, bytes int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == bytes
}
