// Package infra holds the SSH Key feature's driver adapters: the x/crypto/ssh key-material
// implementation, the MongoDB repositories for SSH Keys (with the sealed Deployment Key private
// key) and their provisioner sync records, and the adapter that reaches provisioners through the
// provisioning ProviderFactory. It is the only place in this feature that knows about SSH
// encodings, MongoDB, or the provisioning and site domains; none of their types leak inward.
package infra

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"

	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
)

// minRSABits is the smallest RSA modulus accepted; shorter keys are considered broken.
const minRSABits = 2048

// KeyMaterial implements sshkeydomain.KeyMaterial with golang.org/x/crypto/ssh. It is stateless
// and safe for concurrent use; generation draws from crypto/rand.
type KeyMaterial struct{}

// NewKeyMaterial returns the production key-material adapter.
func NewKeyMaterial() KeyMaterial {
	return KeyMaterial{}
}

// Generate creates an ed25519 key pair and returns its public key with comment and its private key
// as an unencrypted OpenSSH PEM block (the format ssh and ansible-runner read directly).
func (KeyMaterial) Generate(comment string) (sshkeydomain.GeneratedKeyPair, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return sshkeydomain.GeneratedKeyPair{}, fmt.Errorf("generate ed25519 key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(private, cleanComment(comment))
	if err != nil {
		return sshkeydomain.GeneratedKeyPair{}, fmt.Errorf("encode private key: %w", err)
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return sshkeydomain.GeneratedKeyPair{}, fmt.Errorf("encode public key: %w", err)
	}
	return sshkeydomain.GeneratedKeyPair{
		Public:        material(sshPublic, comment),
		PrivateKeyPEM: string(pem.EncodeToMemory(block)),
	}, nil
}

// ParsePublicKey validates one authorized_keys line. Options (from=, command=, …) are refused
// because swallow realizes the key into provisioners that expect a bare key, and a line holding more
// than one key is refused so one record always means one key. The original comment is kept.
func (KeyMaterial) ParsePublicKey(line string) (sshkeydomain.PublicKeyMaterial, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return sshkeydomain.PublicKeyMaterial{}, fmt.Errorf("%w: public key is empty", sshkeydomain.ErrInvalidKey)
	}
	public, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return sshkeydomain.PublicKeyMaterial{}, fmt.Errorf("%w: %v", sshkeydomain.ErrInvalidKey, err)
	}
	if len(options) > 0 {
		return sshkeydomain.PublicKeyMaterial{}, fmt.Errorf("%w: authorized_keys options are not allowed", sshkeydomain.ErrInvalidKey)
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return sshkeydomain.PublicKeyMaterial{}, fmt.Errorf("%w: exactly one public key is expected", sshkeydomain.ErrInvalidKey)
	}
	if err := checkSupported(public); err != nil {
		return sshkeydomain.PublicKeyMaterial{}, err
	}
	return material(public, comment), nil
}

// ParsePrivateKey validates a PEM private key (OpenSSH, PKCS#1, PKCS#8, or SEC1) and derives its
// public key with comment. An encrypted key is ErrPassphraseProtected rather than ErrInvalidKey so
// the operator learns the key is fine but must be decrypted first.
func (KeyMaterial) ParsePrivateKey(privateKeyPEM, comment string) (sshkeydomain.PublicKeyMaterial, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return sshkeydomain.PublicKeyMaterial{}, sshkeydomain.ErrPassphraseProtected
		}
		return sshkeydomain.PublicKeyMaterial{}, fmt.Errorf("%w: %v", sshkeydomain.ErrInvalidKey, err)
	}
	if err := checkSupported(signer.PublicKey()); err != nil {
		return sshkeydomain.PublicKeyMaterial{}, err
	}
	return material(signer.PublicKey(), comment), nil
}

// checkSupported admits ed25519, ECDSA on the NIST curves, and RSA of at least minRSABits. DSA is
// refused as obsolete, and security-key (sk-*) types because swallow cannot use them unattended.
func checkSupported(public ssh.PublicKey) error {
	switch public.Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return nil
	case ssh.KeyAlgoRSA:
		cryptoKey, ok := public.(ssh.CryptoPublicKey)
		if !ok {
			return fmt.Errorf("%w: unreadable RSA key", sshkeydomain.ErrUnsupportedKey)
		}
		rsaKey, ok := cryptoKey.CryptoPublicKey().(*rsa.PublicKey)
		if !ok || rsaKey.N.BitLen() < minRSABits {
			return fmt.Errorf("%w: RSA keys must be at least %d bits", sshkeydomain.ErrUnsupportedKey, minRSABits)
		}
		return nil
	default:
		return fmt.Errorf("%w: %s", sshkeydomain.ErrUnsupportedKey, public.Type())
	}
}

// material renders a public key as swallow stores it: the canonical "<type> <base64>" plus a
// single-line comment, its algorithm name, and its OpenSSH SHA256 fingerprint.
func material(public ssh.PublicKey, comment string) sshkeydomain.PublicKeyMaterial {
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))
	if comment = cleanComment(comment); comment != "" {
		line += " " + comment
	}
	return sshkeydomain.PublicKeyMaterial{
		KeyType:       public.Type(),
		Fingerprint:   ssh.FingerprintSHA256(public),
		AuthorizedKey: line,
	}
}

// cleanComment collapses whitespace so a comment can never break an authorized_keys line in two.
func cleanComment(comment string) string {
	return strings.Join(strings.Fields(comment), " ")
}
