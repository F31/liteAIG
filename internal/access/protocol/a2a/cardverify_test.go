package a2a

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"testing"
)

func newTestCard() *AgentCard {
	return &AgentCard{
		Name: "partner-agent", URL: "https://partner.example/agent",
		Description: "a signed partner card", Version: "2.1.0",
		Skills: []string{"invoice.read"}, Capabilities: []string{"invoice.read"},
		Publisher: "partner.example",
	}
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// signCard attaches a JWS to card, mirroring the documented scheme
// independently of the production verifier so a shared bug cannot make the
// round-trip tests vacuous.
func signCard(t *testing.T, card *AgentCard, alg string, private crypto.Signer) {
	t.Helper()
	unsigned := *card
	unsigned.Signature = nil
	canonical, err := json.Marshal(&unsigned)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	header, err := json.Marshal(map[string]string{"alg": alg})
	if err != nil {
		t.Fatal(err)
	}
	input := b64url(header) + "." + b64url(digest[:])
	messageDigest := sha256.Sum256([]byte(input))
	var value string
	switch alg {
	case AlgRS256:
		key, ok := private.(*rsa.PrivateKey)
		if !ok {
			t.Fatalf("RS256 needs an *rsa.PrivateKey, got %T", private)
		}
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, messageDigest[:])
		if err != nil {
			t.Fatal(err)
		}
		value = b64url(signature)
	case AlgES256:
		key, ok := private.(*ecdsa.PrivateKey)
		if !ok {
			t.Fatalf("ES256 needs an *ecdsa.PrivateKey, got %T", private)
		}
		r, s, err := ecdsa.Sign(rand.Reader, key, messageDigest[:])
		if err != nil {
			t.Fatal(err)
		}
		signature := make([]byte, 64)
		r.FillBytes(signature[:32])
		s.FillBytes(signature[32:])
		value = b64url(signature)
	default:
		t.Fatalf("unknown alg %q", alg)
	}
	card.Signature = &CardSignature{Alg: alg, Value: value}
}

func newRSAPublicKey(t *testing.T) (*rsa.PublicKey, crypto.Signer) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &key.PublicKey, key
}

func newECDSAKey(t *testing.T) (*ecdsa.PublicKey, crypto.Signer) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &key.PublicKey, key
}

func TestVerifyCardSignatureRS256Valid(t *testing.T) {
	card := newTestCard()
	public, private := newRSAPublicKey(t)
	signCard(t, card, AlgRS256, private)
	if err := VerifyCardSignature(card, PublicKey{Algorithm: AlgRS256, Key: public}); err != nil {
		t.Fatalf("valid RS256 signature rejected: %v", err)
	}
}

func TestVerifyCardSignatureES256Valid(t *testing.T) {
	card := newTestCard()
	public, private := newECDSAKey(t)
	signCard(t, card, AlgES256, private)
	if err := VerifyCardSignature(card, PublicKey{Algorithm: AlgES256, Key: public}); err != nil {
		t.Fatalf("valid ES256 signature rejected: %v", err)
	}
}

func TestVerifyCardSignatureWrongKeyFails(t *testing.T) {
	card := newTestCard()
	_, signerPrivate := newRSAPublicKey(t)
	signCard(t, card, AlgRS256, signerPrivate)
	otherPublic, _ := newRSAPublicKey(t)
	if err := VerifyCardSignature(card, PublicKey{Algorithm: AlgRS256, Key: otherPublic}); !errors.Is(err, ErrVerification) {
		t.Fatalf("wrong key err = %v, want ErrVerification", err)
	}
}

func TestVerifyCardSignatureWrongAlgFails(t *testing.T) {
	card := newTestCard()
	_, rsaPrivate := newRSAPublicKey(t)
	signCard(t, card, AlgRS256, rsaPrivate)
	// Only an ES256 key is offered: the card's RS256 matches nothing, so the
	// verification fails closed before any signature math runs.
	ecdsaPublic, _ := newECDSAKey(t)
	if err := VerifyCardSignature(card, PublicKey{Algorithm: AlgES256, Key: ecdsaPublic}); !errors.Is(err, ErrNoMatchingKey) {
		t.Fatalf("wrong alg err = %v, want ErrNoMatchingKey", err)
	}
}

func TestVerifyCardSignatureUnsignedFails(t *testing.T) {
	card := newTestCard()
	public, _ := newRSAPublicKey(t)
	if err := VerifyCardSignature(card, PublicKey{Algorithm: AlgRS256, Key: public}); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("unsigned err = %v, want ErrUnsigned", err)
	}
	card.Signature = &CardSignature{Alg: AlgRS256, Value: ""}
	if err := VerifyCardSignature(card, PublicKey{Algorithm: AlgRS256, Key: public}); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("empty signature err = %v, want ErrUnsigned", err)
	}
}

func TestVerifyCardSignatureUnsupportedAlgFails(t *testing.T) {
	card := newTestCard()
	public, _ := newRSAPublicKey(t)
	card.Signature = &CardSignature{Alg: "HS256", Value: "bogus"}
	err := VerifyCardSignature(card, PublicKey{Algorithm: AlgRS256, Key: public})
	if !errors.Is(err, ErrUnsupportedAlg) && !errors.Is(err, ErrNoMatchingKey) {
		t.Fatalf("unsupported alg err = %v, want ErrUnsupportedAlg/ErrNoMatchingKey", err)
	}
}

func TestVerifyCardSignatureTamperedTextFails(t *testing.T) {
	card := newTestCard()
	public, private := newRSAPublicKey(t)
	signCard(t, card, AlgRS256, private)
	// Tamper with a covered field after signing.
	card.Description = "tampered"
	if err := VerifyCardSignature(card, PublicKey{Algorithm: AlgRS256, Key: public}); !errors.Is(err, ErrVerification) {
		t.Fatalf("tampered card err = %v, want ErrVerification", err)
	}
}

// TestVerifyCardSignatureES256Vector pins an externally produced ES256 vector:
// a fixed P-256 public key and a signature over a fixed card, so ES256
// verification is proven against material produced outside this package's own
// sign helper.
func TestVerifyCardSignatureES256Vector(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(es256VectorCard), &raw); err != nil {
		t.Fatal(err)
	}
	// Re-marshal and unmarshal through AgentCard so wire order exercises the
	// canonical form exactly as a discovery client would receive it.
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var card AgentCard
	if err := json.Unmarshal(rawJSON, &card); err != nil {
		t.Fatal(err)
	}
	x := decodePoint(t, es256VectorX)
	y := decodePoint(t, es256VectorY)
	public := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	if err := VerifyCardSignature(&card, PublicKey{Algorithm: AlgES256, Key: public}); err != nil {
		t.Fatalf("fixed ES256 vector rejected: %v", err)
	}
	// The vector's covered bytes are exactly the canonical card JSON.
	got, err := canonicalSigningBytes(&card)
	if err != nil {
		t.Fatal(err)
	}
	var canonical map[string]any
	if err := json.Unmarshal(got, &canonical); err != nil {
		t.Fatal(err)
	}
	if canonical["name"] != "vector-agent" || canonical["version"] != "1.2.3" {
		t.Fatalf("unexpected canonical content: %s", got)
	}
}

func decodePoint(t *testing.T, encoded string) *big.Int {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return new(big.Int).SetBytes(raw)
}

// TestCanonicalSigningBytesStableUnderSignature proves stripping the signature
// never perturbs the covered bytes: a signed card canonicalizes identically to
// the same card without its signature.
func TestCanonicalSigningBytesStableUnderSignature(t *testing.T) {
	card := newTestCard()
	_, private := newRSAPublicKey(t)
	signed, err := canonicalSigningBytes(card)
	if err != nil {
		t.Fatal(err)
	}
	signCard(t, card, AlgRS256, private)
	covered, err := canonicalSigningBytes(card)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(signed, covered) {
		t.Fatalf("signature perturbs canonical bytes:\nwithout sig: %s\nwith sig:    %s", signed, covered)
	}
}

// TestVerifyCardSignatureNilCardAndNilSignature guards the failure contract.
func TestVerifyCardSignatureNilCardAndNilSignature(t *testing.T) {
	if err := VerifyCardSignature(nil); err == nil {
		t.Fatal("nil card must fail")
	}
	card := newTestCard()
	if err := VerifyCardSignature(card); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("no keys err = %v, want ErrUnsigned", err)
	}
}

// es256Vector constants generated once from a fixed P-256 key and card (see
// the generator note in the git history); they pin wire-level ES256 behavior.
const (
	es256VectorCard = `{"name":"vector-agent","url":"https://vector.example/agent","description":"fixed ES256 verification vector","version":"1.2.3","skills":["vector.skill"],"capabilities":["vector.cap"],"publisher":"vector.example","signature":{"alg":"ES256","value":"FQFj4UVJ6tFeStkZk02URTGs3siV6-LGfG_dQv9jm6eWVzOGv03hNagAmJhYSoYfM3XPaeoRsCzaaRD9yK4rew"}}`
	es256VectorX    = "DzHttagq1AX2dc98m8LulFpCt9x5b9QwaN5GIWLwIz8"
	es256VectorY    = "IAaPcsXwBxeWDJ6_AZnZXk59cqevc9R1XGQxuxM0PVQ"
)
