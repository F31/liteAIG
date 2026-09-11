package a2a

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

// Signature algorithms supported by the Agent Card JWS scheme. RS256 uses
// PKCS#1 v1.5 RSA over SHA-256; ES256 uses ECDSA over P-256 with the raw
// r||s encoding.
const (
	AlgRS256 = "RS256"
	AlgES256 = "ES256"
)

// Signature errors are low-cardinality and never embed card contents, URLs, or
// signature material so they are safe to propagate into logs and error paths.
var (
	// ErrUnsigned reports a card that carries no signature to verify.
	ErrUnsigned = errors.New("agent card is unsigned")
	// ErrUnsupportedAlg reports a signature algorithm outside the supported set.
	ErrUnsupportedAlg = errors.New("unsupported agent card signature algorithm")
	// ErrNoMatchingKey reports that none of the supplied keys matches the card's algorithm.
	ErrNoMatchingKey = errors.New("no public key matches the agent card signature algorithm")
	// ErrVerification reports that the signature did not verify against the card.
	ErrVerification = errors.New("agent card signature verification failed")
)

// PublicKey is a verification key for signed Agent Cards. Algorithm is one of
// AlgRS256 or AlgES256; Key holds the corresponding *rsa.PublicKey or
// *ecdsa.PublicKey.
type PublicKey struct {
	Algorithm string
	Key       any
}

// canonicalSigningBytes renders the deterministic bytes a card signature
// covers: the card marshaled without its Signature field. Struct field order
// is fixed by the type, so identical card content yields identical bytes and
// stripping the signature (or verifying an already-signed card) is stable.
func canonicalSigningBytes(card *AgentCard) ([]byte, error) {
	unsigned := *card
	unsigned.Signature = nil
	return json.Marshal(&unsigned)
}

// protectedHeader renders the minimal JWS protected header for alg.
func protectedHeader(alg string) ([]byte, error) {
	return json.Marshal(map[string]string{"alg": alg})
}

// signingInput builds the JWS signing input
//
//	base64url(header) + "." + base64url(sha256(canonicalSigningBytes(card)))
//
// The card itself is never the JWS payload: only its SHA-256 digest is, which
// keeps signed cards small and makes the covered bytes unambiguous. The header
// is rebuilt deterministically from the card's algorithm, so the signature
// binds the algorithm to the content.
func signingInput(card *AgentCard) ([]byte, error) {
	if card.Signature == nil {
		return nil, ErrUnsigned
	}
	header, err := protectedHeader(card.Signature.Alg)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalSigningBytes(card)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	input := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(digest[:])
	return []byte(input), nil
}

// VerifyCardSignature verifies card.Signature (a JWS over the card's canonical
// signing bytes) against the first supplied key whose Algorithm matches the
// card's declared algorithm. An unsigned card fails with ErrUnsigned; a card
// whose algorithm matches no key fails with ErrNoMatchingKey; any signature
// that does not verify fails with ErrVerification. Keys are tried in order, so
// callers pin trust by the order they pass known keys.
func VerifyCardSignature(card *AgentCard, keys ...PublicKey) error {
	if card == nil {
		return errors.New("nil agent card")
	}
	if card.Signature == nil || card.Signature.Value == "" {
		return ErrUnsigned
	}
	input, err := signingInput(card)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(input)
	alg := card.Signature.Alg
	var key any
	for _, candidate := range keys {
		if candidate.Algorithm == alg {
			key = candidate.Key
			break
		}
	}
	if key == nil {
		return ErrNoMatchingKey
	}
	signature, err := base64.RawURLEncoding.DecodeString(card.Signature.Value)
	if err != nil {
		return fmt.Errorf("%w: invalid signature encoding", ErrVerification)
	}
	switch alg {
	case AlgRS256:
		public, ok := key.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: key is not an RSA public key", ErrNoMatchingKey)
		}
		if err := rsa.VerifyPKCS1v15(public, crypto.SHA256, digest[:], signature); err != nil {
			return fmt.Errorf("%w: %v", ErrVerification, err)
		}
		return nil
	case AlgES256:
		public, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: key is not an ECDSA public key", ErrNoMatchingKey)
		}
		if !validES256Signature(public, digest[:], signature) {
			return ErrVerification
		}
		return nil
	default:
		return ErrUnsupportedAlg
	}
}

// validES256Signature verifies a fixed 64-byte r||s ECDSA signature over
// digest against public. Signatures of any other length cannot verify.
func validES256Signature(public *ecdsa.PublicKey, digest, signature []byte) bool {
	if public.Curve != elliptic.P256() {
		return false
	}
	if len(signature) != 64 {
		return false
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	return ecdsa.Verify(public, digest, r, s)
}
