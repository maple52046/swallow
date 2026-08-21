// Package secret seals integration credentials for storage.
//
// gdcm holds exactly one class of secret it cannot avoid: the credentials it uses to
// authenticate to the systems it integrates with. Those are sealed here so that a
// database dump alone does not yield working credentials, and so that every read path
// has to go through an explicit Open call rather than reading a field.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// keyBytes is the AES-256 key length. Only one key size is accepted so that a short
// key is a configuration error rather than a silently weaker cipher.
const keyBytes = 32

// Sealer encrypts and decrypts credentials with AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer builds a Sealer from a base64-encoded 32-byte key.
func NewSealer(base64Key string) (*Sealer, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("credential key is not valid base64: %w", err)
	}
	if len(key) != keyBytes {
		return nil, fmt.Errorf(
			"credential key must decode to %d bytes, got %d; generate one with: openssl rand -base64 32",
			keyBytes, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build gcm: %w", err)
	}

	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext and returns a base64 string safe to store as one field.
// The nonce is prepended to the ciphertext, so no separate nonce column is needed.
func (s *Sealer) Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", errors.New("refusing to seal an empty credential")
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	sealed := s.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal.
//
// A failure here usually means the configured credential key is not the one the value
// was sealed with, so the error says that rather than reporting a generic cipher fault.
func (s *Sealer) Open(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("stored credential is not valid base64: %w", err)
	}

	nonceSize := s.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("stored credential is truncated")
	}

	plaintext, err := s.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", errors.New("cannot decrypt stored credential; the configured credential key does not match the one it was sealed with")
	}
	return string(plaintext), nil
}
