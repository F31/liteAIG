package a2a

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var (
	ErrPushSignatureMissing   = errors.New("a2a push signature missing")
	ErrPushSignatureMalformed = errors.New("a2a push signature malformed")
	ErrPushTimestampInvalid   = errors.New("a2a push timestamp invalid")
	ErrPushTimestampSkew      = errors.New("a2a push timestamp outside allowed skew")
	ErrPushSignatureMismatch  = errors.New("a2a push signature mismatch")
)

// SignPushPayload returns the hex HMAC-SHA256 value covered by the LiteAIG A2A
// push profile: timestamp + "\n" + raw JSON payload bytes. Callers put the
// returned value in the header as "sha256=<hex>".
func SignPushPayload(secret []byte, timestamp string, payload []byte) string {
	return SignPushPayloadWithID(secret, "", timestamp, payload)
}

// SignPushPayloadWithID signs the durable push profile. When deliveryID is not
// empty it is included between timestamp and payload, allowing receivers to
// deduplicate by id and reject payloads replayed under another delivery id.
func SignPushPayloadWithID(secret []byte, deliveryID, timestamp string, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	if deliveryID != "" {
		mac.Write([]byte(deliveryID))
		mac.Write([]byte("\n"))
	}
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyPushPayloadSignature validates the LiteAIG A2A push signature headers.
// signature must be the full header value ("sha256=<hex>"). maxSkew <= 0 skips
// timestamp freshness checks but still validates RFC3339/RFC3339Nano syntax.
func VerifyPushPayloadSignature(secret []byte, timestamp, signature string, payload []byte, now time.Time, maxSkew time.Duration) error {
	return VerifyPushPayloadSignatureWithID(secret, "", timestamp, signature, payload, now, maxSkew)
}

// VerifyPushPayloadSignatureWithID validates the durable push signature. The
// caller should also persist deliveryID values it has accepted and reject
// duplicates to obtain replay protection inside the timestamp skew window.
func VerifyPushPayloadSignatureWithID(secret []byte, deliveryID, timestamp, signature string, payload []byte, now time.Time, maxSkew time.Duration) error {
	if signature == "" {
		return ErrPushSignatureMissing
	}
	encoded, ok := strings.CutPrefix(signature, "sha256=")
	if !ok || encoded == "" {
		return ErrPushSignatureMalformed
	}
	got, err := hex.DecodeString(encoded)
	if err != nil {
		return ErrPushSignatureMalformed
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return ErrPushTimestampInvalid
	}
	if maxSkew > 0 {
		if now.IsZero() {
			now = time.Now()
		}
		delta := now.Sub(parsed)
		if delta < 0 {
			delta = -delta
		}
		if delta > maxSkew {
			return ErrPushTimestampSkew
		}
	}
	expected, err := hex.DecodeString(SignPushPayloadWithID(secret, deliveryID, timestamp, payload))
	if err != nil {
		return ErrPushSignatureMalformed
	}
	if !hmac.Equal(got, expected) {
		return ErrPushSignatureMismatch
	}
	return nil
}
