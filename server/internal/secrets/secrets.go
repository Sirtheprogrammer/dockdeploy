// Package secrets seals credentials for storage at rest.
//
// Everything sensitive the platform holds on a user's behalf -- SSH private
// keys, sudo passwords, registry passwords, git tokens, deployment env values
// -- is encrypted with AES-256-GCM before it reaches Postgres and is only
// opened at the moment it is used. Plaintext must never leave the process
// through an API response or a log line.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// ErrTampered means the ciphertext, nonce, or associated data did not
// authenticate. GCM cannot distinguish a corrupted value from a forged one, so
// callers should treat this as a security event rather than a decode failure.
var ErrTampered = errors.New("secrets: ciphertext failed authentication")

// Kind labels what a stored secret is, so the UI can describe a credential
// without ever decrypting it.
type Kind string

const (
	KindSSHPassword   Kind = "ssh_password"
	KindSSHPrivateKey Kind = "ssh_private_key"
	KindSSHPassphrase Kind = "ssh_passphrase"
	KindSudoPassword  Kind = "sudo_password"
	KindGitToken      Kind = "git_token"
	KindRegistryToken Kind = "registry_token"
	KindDeploymentEnv Kind = "deployment_env"
	KindWebhookSecret Kind = "webhook_secret"
)

// Sealer encrypts and decrypts with a single process-wide key.
type Sealer struct {
	aead cipher.AEAD
}

// KeySize is the required key length. aes.NewCipher also accepts 16 and 24
// byte keys, which would silently give AES-128 or AES-192 instead, so the
// length is pinned here rather than left to the cipher.
const KeySize = 32

// NewSealer builds a Sealer from a 32 byte key (see config.EncryptionKeySize).
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: build GCM: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext and returns a fresh random nonce alongside the
// ciphertext.
//
// aad is authenticated but not encrypted. Callers pass the row identity (the
// secret's UUID) so a ciphertext moved to a different row stops decrypting --
// without it, an attacker with write access to the table could swap one
// server's credential onto another server.
func (s *Sealer) Seal(plaintext, aad []byte) (nonce, ciphertext []byte, err error) {
	nonce = make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("secrets: read nonce: %w", err)
	}
	return nonce, s.aead.Seal(nil, nonce, plaintext, aad), nil
}

// SealString is Seal for the common case of a textual credential.
func (s *Sealer) SealString(plaintext string, aad []byte) (nonce, ciphertext []byte, err error) {
	return s.Seal([]byte(plaintext), aad)
}

// Open reverses Seal. It returns ErrTampered if authentication fails.
func (s *Sealer) Open(nonce, ciphertext, aad []byte) ([]byte, error) {
	if len(nonce) != s.aead.NonceSize() {
		return nil, ErrTampered
	}
	plaintext, err := s.aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrTampered
	}
	return plaintext, nil
}

// OpenString is Open for textual credentials.
func (s *Sealer) OpenString(nonce, ciphertext, aad []byte) (string, error) {
	plaintext, err := s.Open(nonce, ciphertext, aad)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
