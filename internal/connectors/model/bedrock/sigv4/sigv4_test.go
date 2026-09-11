package sigv4

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignAddsAuthorizationAndAmzHeaders(t *testing.T) {
	now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-3-5-haiku/invoke", strings.NewReader(`{"anthropic_version":"bedrock-2023-05-31"}`))
	if err != nil {
		t.Fatal(err)
	}
	signer := New(Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}, "bedrock", "us-east-1", func() time.Time { return now })
	signer.Sign(req, "", time.Time{})
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20230101/us-east-1/bedrock/aws4_request") {
		t.Fatalf("authorization header = %q", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=x-amz-content-sha256;x-amz-date") {
		t.Fatalf("signed headers missing: %q", auth)
	}
	if req.Header.Get("X-Amz-Content-Sha256") == "" || req.Header.Get("X-Amz-Date") != "20230101T000000Z" {
		t.Fatalf("amz headers missing: date=%q sha=%q", req.Header.Get("X-Amz-Date"), req.Header.Get("X-Amz-Content-Sha256"))
	}
	if signature := auth[strings.LastIndex(auth, "Signature=")+len("Signature="):]; len(signature) != 64 {
		t.Fatalf("signature length = %d, want 64 hex chars", len(signature))
	}
}

// The canonical URI keeps the model-id path intact (SigV4 encodes each path
// segment; plain model IDs contain only safe characters).
func TestCanonicalURIPreservesModelPath(t *testing.T) {
	if got := canonicalURI("/model/anthropic.claude-3-5-haiku/invoke"); got != "/model/anthropic.claude-3-5-haiku/invoke" {
		t.Fatalf("canonicalURI = %q", got)
	}
}

func TestCanonicalQuerySorts(t *testing.T) {
	// Encoded query strings round-trip through url.Values sorting.
	values := url.Values{}
	values.Set("b", "2")
	values.Set("a", "1")
	got := canonicalQuery(values.Encode())
	if got != "a=1&b=2" {
		t.Fatalf("canonicalQuery = %q", got)
	}
}
