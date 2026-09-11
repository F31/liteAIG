// Package id provides opaque CSPRNG identifiers.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

type Generator struct {
	random io.Reader
}

func NewGenerator(random io.Reader) *Generator {
	if random == nil {
		random = rand.Reader
	}
	return &Generator{random: random}
}

func (g *Generator) New() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(g.random, value); err != nil {
		return "", fmt.Errorf("generate identifier: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}

func (g *Generator) NewOpaque(byteCount int) (string, error) {
	if byteCount <= 0 {
		return "", fmt.Errorf("opaque identifier byte count must be positive")
	}
	value := make([]byte, byteCount)
	if _, err := io.ReadFull(g.random, value); err != nil {
		return "", fmt.Errorf("generate opaque identifier: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func (g *Generator) NewTenantRef() (string, error) { return g.NewOpaque(16) }
