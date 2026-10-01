package infra

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
)

func TestGenerate_ProducesUsableEd25519Pair(t *testing.T) {
	pair, err := NewKeyMaterial().Generate("swallow deployment")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if pair.Public.KeyType != ssh.KeyAlgoED25519 {
		t.Errorf("KeyType = %q, want %q", pair.Public.KeyType, ssh.KeyAlgoED25519)
	}
	if !strings.HasSuffix(pair.Public.AuthorizedKey, " swallow deployment") {
		t.Errorf("AuthorizedKey = %q, want the comment appended", pair.Public.AuthorizedKey)
	}
	if !strings.HasPrefix(pair.PrivateKeyPEM, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Errorf("private key is not an OpenSSH PEM block")
	}
	signer, err := ssh.ParsePrivateKey([]byte(pair.PrivateKeyPEM))
	if err != nil {
		t.Fatalf("generated private key does not parse: %v", err)
	}
	if got := ssh.FingerprintSHA256(signer.PublicKey()); got != pair.Public.Fingerprint {
		t.Errorf("private key fingerprint = %s, want it to match the public key %s", got, pair.Public.Fingerprint)
	}
}

func TestParsePrivateKey_DerivesPublicKey(t *testing.T) {
	material := NewKeyMaterial()
	pair, err := material.Generate("original")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	parsed, err := material.ParsePrivateKey(pair.PrivateKeyPEM, "renamed")
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	if parsed.Fingerprint != pair.Public.Fingerprint {
		t.Errorf("ParsePrivateKey fingerprint = %s, want %s", parsed.Fingerprint, pair.Public.Fingerprint)
	}
	if !strings.HasSuffix(parsed.AuthorizedKey, " renamed") {
		t.Errorf("AuthorizedKey = %q, want the supplied comment", parsed.AuthorizedKey)
	}
}

func TestParsePrivateKey_RejectsEncryptedAndGarbage(t *testing.T) {
	material := NewKeyMaterial()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	// Legacy (deprecated) PEM encryption is exactly what an operator may paste; it must be refused.
	encrypted, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(rsaKey), []byte("secret"), x509.PEMCipherAES256)
	if err != nil {
		t.Fatalf("EncryptPEMBlock: %v", err)
	}
	if _, err := material.ParsePrivateKey(string(pem.EncodeToMemory(encrypted)), "x"); !errors.Is(err, sshkeydomain.ErrPassphraseProtected) {
		t.Errorf("encrypted key error = %v, want ErrPassphraseProtected", err)
	}
	if _, err := material.ParsePrivateKey("not a key", "x"); !errors.Is(err, sshkeydomain.ErrInvalidKey) {
		t.Errorf("garbage key error = %v, want ErrInvalidKey", err)
	}
}

func TestParsePublicKey_Validation(t *testing.T) {
	material := NewKeyMaterial()
	pair, err := material.Generate("alice@laptop")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	weakPublic, err := ssh.NewPublicKey(&weak.PublicKey)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}

	tests := []struct {
		name    string
		line    string
		wantErr error
	}{
		{name: "valid ed25519 keeps comment", line: pair.Public.AuthorizedKey},
		{name: "empty", line: "  ", wantErr: sshkeydomain.ErrInvalidKey},
		{name: "not a key", line: "hello world", wantErr: sshkeydomain.ErrInvalidKey},
		{name: "options refused", line: `from="10.0.0.1" ` + pair.Public.AuthorizedKey, wantErr: sshkeydomain.ErrInvalidKey},
		{name: "two keys refused", line: pair.Public.AuthorizedKey + "\n" + pair.Public.AuthorizedKey, wantErr: sshkeydomain.ErrInvalidKey},
		{name: "weak rsa refused", line: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(weakPublic))), wantErr: sshkeydomain.ErrUnsupportedKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := material.ParsePublicKey(tc.line)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ParsePublicKey() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got.AuthorizedKey != pair.Public.AuthorizedKey {
				t.Errorf("ParsePublicKey() = %q, want %q", got.AuthorizedKey, pair.Public.AuthorizedKey)
			}
		})
	}
}
