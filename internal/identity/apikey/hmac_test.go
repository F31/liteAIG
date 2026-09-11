package apikey

import "testing"

func TestVersionedPepperHMAC(t *testing.T) {
	token := Token{TenantRef: "11111111111111111111111111111111", PublicID: "22222222222222222222222222222222", Secret: "3333333333333333333333333333333333333333333333333333333333333333"}
	digest := Digest([]byte("pepper-v1"), token)
	if !Verify([]byte("pepper-v1"), token, digest) {
		t.Fatal("Verify() rejected correct pepper")
	}
	if Verify([]byte("pepper-v2"), token, digest) {
		t.Fatal("Verify() accepted wrong pepper")
	}
}
