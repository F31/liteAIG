package password

import (
	"bytes"
	"strings"
	"testing"
)

func TestArgon2idHashAndVerify(t *testing.T) {
	hasher, err := NewArgon2id(Argon2idParams{
		MemoryKiB:   64,
		Iterations:  1,
		Parallelism: 1,
		SaltBytes:   16,
		KeyBytes:    32,
	}, bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("correct horse battery staple")
	encoded, err := hasher.Hash(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, string(plaintext)) {
		t.Fatal("encoded hash contains plaintext password")
	}
	matched, err := hasher.Verify(plaintext, encoded)
	if err != nil || !matched {
		t.Fatalf("Verify() = %t, %v", matched, err)
	}
	matched, err = hasher.Verify([]byte("wrong password"), encoded)
	if err != nil || matched {
		t.Fatalf("Verify(wrong) = %t, %v", matched, err)
	}
}

func TestArgon2idRejectsMalformedEncoding(t *testing.T) {
	hasher, err := NewArgon2id(DefaultArgon2idParams(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hasher.Verify([]byte("password"), "$argon2id$invalid"); err == nil {
		t.Fatal("Verify() accepted malformed encoding")
	}
}
