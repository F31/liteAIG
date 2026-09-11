package apikey

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func Digest(pepper []byte, token Token) []byte {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(token.TenantRef + "." + token.PublicID + "." + token.Secret))
	return mac.Sum(nil)
}

func Verify(pepper []byte, token Token, expected []byte) bool {
	return hmac.Equal(Digest(pepper, token), expected)
}

func Fingerprint(token Token) string {
	sum := sha256.Sum256([]byte(token.String()))
	return hex.EncodeToString(sum[:16])
}
