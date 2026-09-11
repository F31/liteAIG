package a2a

import (
	"errors"
	"testing"
	"time"
)

func TestPushPayloadSignatureVerify(t *testing.T) {
	secret := []byte("shared-secret")
	deliveryID := "push-123"
	timestamp := "2026-09-08T12:00:00Z"
	payload := []byte(`{"jsonrpc":"2.0","result":{"ok":true}}`)
	signature := "sha256=" + SignPushPayloadWithID(secret, deliveryID, timestamp, payload)
	if err := VerifyPushPayloadSignatureWithID(secret, deliveryID, timestamp, signature, payload, time.Date(2026, 9, 8, 12, 0, 30, 0, time.UTC), time.Minute); err != nil {
		t.Fatalf("VerifyPushPayloadSignatureWithID() = %v", err)
	}
	if err := VerifyPushPayloadSignature(secret, timestamp, "sha256="+SignPushPayload(secret, timestamp, payload), payload, time.Date(2026, 9, 8, 12, 0, 30, 0, time.UTC), time.Minute); err != nil {
		t.Fatalf("legacy VerifyPushPayloadSignature() = %v", err)
	}
}

func TestPushPayloadSignatureRejectsTamperAndSkew(t *testing.T) {
	secret := []byte("shared-secret")
	timestamp := "2026-09-08T12:00:00Z"
	payload := []byte(`{"ok":true}`)
	signature := "sha256=" + SignPushPayload(secret, timestamp, payload)

	if err := VerifyPushPayloadSignature(secret, timestamp, signature, []byte(`{"ok":false}`), time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), time.Minute); !errors.Is(err, ErrPushSignatureMismatch) {
		t.Fatalf("tampered payload error = %v", err)
	}
	if err := VerifyPushPayloadSignature(secret, timestamp, signature, payload, time.Date(2026, 9, 8, 12, 2, 0, 0, time.UTC), time.Minute); !errors.Is(err, ErrPushTimestampSkew) {
		t.Fatalf("skew error = %v", err)
	}
	if err := VerifyPushPayloadSignature(secret, "bad", signature, payload, time.Time{}, 0); !errors.Is(err, ErrPushTimestampInvalid) {
		t.Fatalf("timestamp error = %v", err)
	}
	if err := VerifyPushPayloadSignature(secret, timestamp, "bad", payload, time.Time{}, 0); !errors.Is(err, ErrPushSignatureMalformed) {
		t.Fatalf("malformed signature error = %v", err)
	}
}

func TestPushPayloadSignatureDeliveryIDBinding(t *testing.T) {
	secret := []byte("shared-secret")
	timestamp := "2026-09-08T12:00:00Z"
	payload := []byte(`{"ok":true}`)
	signature := "sha256=" + SignPushPayloadWithID(secret, "push-a", timestamp, payload)
	if err := VerifyPushPayloadSignatureWithID(secret, "push-b", timestamp, signature, payload, time.Time{}, 0); !errors.Is(err, ErrPushSignatureMismatch) {
		t.Fatalf("different delivery id error = %v", err)
	}
}
