package secret

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func testKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestSealAndOpen(t *testing.T) {
	sealer, err := NewSealer(testKey(t))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	const credential = "consumer-key:token-key:token-secret"
	sealed, err := sealer.Seal(credential)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, "token-secret") {
		t.Fatal("the sealed value must not contain the plaintext")
	}

	opened, err := sealer.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened != credential {
		t.Fatalf("round trip failed: got %q", opened)
	}
}

// A fresh nonce per call means the same credential seals to a different value each
// time, so equal ciphertexts cannot be used to infer equal credentials.
func TestSeal_IsNotDeterministic(t *testing.T) {
	sealer, err := NewSealer(testKey(t))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	first, err := sealer.Seal("same-credential")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	second, err := sealer.Seal("same-credential")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if first == second {
		t.Fatal("sealing the same value twice must not produce the same ciphertext")
	}
}

func TestOpen_WithWrongKeyExplainsWhy(t *testing.T) {
	original, _ := NewSealer(testKey(t))
	other, _ := NewSealer(testKey(t))

	sealed, err := original.Seal("credential")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	_, err = other.Open(sealed)
	if err == nil {
		t.Fatal("expected decryption with the wrong key to fail")
	}
	// The likely cause is a rotated or mistyped key, and the message should say so
	// rather than reporting a generic cipher fault.
	if !strings.Contains(err.Error(), "credential key") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestOpen_RejectsTamperedCiphertext(t *testing.T) {
	sealer, _ := NewSealer(testKey(t))

	sealed, err := sealer.Seal("credential")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[len(raw)-1] ^= 0xff
	tampered := base64.StdEncoding.EncodeToString(raw)

	if _, err := sealer.Open(tampered); err == nil {
		t.Fatal("GCM must reject a modified ciphertext")
	}
}

func TestNewSealer_RejectsBadKeys(t *testing.T) {
	cases := map[string]string{
		"not base64": "!!!not-base64!!!",
		"too short":  base64.StdEncoding.EncodeToString([]byte("short")),
		"empty":      "",
	}

	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewSealer(key); err == nil {
				t.Fatalf("expected an error for %s", name)
			}
		})
	}
}

// A short key must be an error rather than a silently weaker cipher.
func TestNewSealer_ErrorTellsYouHowToMakeAKey(t *testing.T) {
	_, err := NewSealer(base64.StdEncoding.EncodeToString([]byte("too-short")))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "openssl rand -base64 32") {
		t.Errorf("the error should tell an operator how to generate a key: %v", err)
	}
}

func TestSeal_RefusesEmptyCredential(t *testing.T) {
	sealer, _ := NewSealer(testKey(t))

	if _, err := sealer.Seal(""); err == nil {
		t.Fatal("sealing an empty credential would store something that decrypts to nothing")
	}
}
