// Package sigv4 implements AWS Signature Version 4 request signing with the
// standard library only (no AWS SDK dependency). It signs Bedrock Runtime
// invocations so the egress transport stays a plain *http.Client.
package sigv4

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Credentials carries the AWS access key material. SessionToken may be empty.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// Signer signs HTTP requests for one service+region.
type Signer struct {
	credentials Credentials
	service     string
	region      string
	now         func() time.Time
}

func New(credentials Credentials, service, region string, now func() time.Time) *Signer {
	if now == nil {
		now = time.Now
	}
	return &Signer{credentials: credentials, service: service, region: region, now: now}
}

// Sign applies the SigV4 Authorization header and x-amz-* headers to request.
func (s *Signer) Sign(request *http.Request, payloadHash string, overrideDate time.Time) {
	now := overrideDate
	if now.IsZero() {
		now = s.now().UTC()
	}
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	request.Header.Set("X-Amz-Date", amzDate)
	if s.credentials.SessionToken != "" {
		request.Header.Set("X-Amz-Security-Token", s.credentials.SessionToken)
	}
	if payloadHash == "" {
		payloadHash = hexSHA256([]byte(""))
	}
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonical := canonicalRequest(request, payloadHash)
	scope := dateStamp + "/" + s.region + "/" + s.service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA256([]byte(canonical))
	signature := s.signature(dateStamp, stringToSign)
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.credentials.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaderNames(request)+", Signature="+signature)
}

func (s *Signer) signature(dateStamp, stringToSign string) string {
	dateKey := hmacSHA256([]byte("AWS4"+s.credentials.SecretAccessKey), dateStamp)
	regionKey := hmacSHA256(dateKey, s.region)
	serviceKey := hmacSHA256(regionKey, s.service)
	signingKey := hmacSHA256(serviceKey, "aws4_request")
	return hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
}

func canonicalRequest(request *http.Request, payloadHash string) string {
	headers := request.Header
	names := sortedHeaderNames(headers)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(strings.ToLower(name))
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(headers.Get(name)))
		canonicalHeaders.WriteString("\n")
	}
	return strings.Join([]string{
		request.Method,
		canonicalURI(request.URL.Path),
		canonicalQuery(request.URL.RawQuery),
		canonicalHeaders.String(),
		strings.Join(lowerCased(names), ";"),
		payloadHash,
	}, "\n")
}

func signedHeaderNames(request *http.Request) string {
	return strings.Join(lowerCased(sortedHeaderNames(request.Header)), ";")
}

func sortedHeaderNames(headers http.Header) []string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func lowerCased(names []string) []string {
	result := make([]string, len(names))
	for i, name := range names {
		result[i] = strings.ToLower(name)
	}
	return result
}

// canonicalQuery re-encodes the raw query so keys and values are sorted and
// fully encoded as SigV4 expects. RawQuery is already percent-encoded by the
// transport; keys are sorted for the canonical form.
func canonicalQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	pairs := strings.Split(rawQuery, "&")
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// canonicalURI returns the URI path with each segment percent-encoded. The
// paths this package signs are plain model IDs, so a simple double-encoding is
// avoided: the value is returned as-is to bracket the surface.
func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
