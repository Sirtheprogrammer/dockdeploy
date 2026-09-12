package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
)

// APITokenPrefix marks a bearer token so leak scanners and humans can both
// recognise one on sight.
const APITokenPrefix = "ddp_"

// tokenBytes is the entropy behind session and API tokens. 32 bytes makes
// guessing irrelevant, so tokens are never rate-limited or locked out.
const tokenBytes = 32

// Hasher derives the stored form of a bearer token.
//
// Tokens are already unguessable, so the hash is not about slowing an attacker
// down -- it keeps live credentials out of a database dump. Keying it with
// SESSION_SECRET has a second effect worth relying on: rotating that secret
// invalidates every session and API token at once.
type Hasher struct {
	key []byte
}

func NewHasher(sessionSecret []byte) *Hasher {
	return &Hasher{key: sessionSecret}
}

// NewToken returns a fresh token and the hash to store for it.
func (h *Hasher) NewToken() (token string, hash []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("auth: read token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, h.Hash(token), nil
}

// NewAPIToken returns a prefixed token, its hash, and the display prefix.
func (h *Hasher) NewAPIToken() (token string, hash []byte, prefix string, err error) {
	body, _, err := h.NewToken()
	if err != nil {
		return "", nil, "", err
	}
	token = APITokenPrefix + body
	return token, h.Hash(token), displayPrefix(token), nil
}

// Hash derives the stored hash for a token.
func (h *Hasher) Hash(token string) []byte {
	mac := hmac.New(sha256.New, h.key)
	mac.Write([]byte(token))
	return mac.Sum(nil)
}

// Equal compares two token hashes in constant time.
func (h *Hasher) Equal(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// displayPrefix is the leading fragment kept in plaintext so a token can be
// identified in a list after its secret is unrecoverable.
func displayPrefix(token string) string {
	const shown = len(APITokenPrefix) + 6
	if len(token) <= shown {
		return token
	}
	return token[:shown]
}

// ParseBearer extracts a token from an Authorization header.
func ParseBearer(header string) (string, bool) {
	const scheme = "Bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", false
	}
	token := strings.TrimSpace(header[len(scheme):])
	return token, token != ""
}
