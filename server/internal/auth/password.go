// Package auth handles identity: password hashing, session and API tokens, and
// the role-to-permission matrix every route is checked against.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. 64 MiB with two passes is the OWASP baseline; it costs
// roughly 50ms per login on modest hardware, which is the point.
const (
	argonMemory  uint32 = 64 * 1024
	argonTime    uint32 = 2
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// MinPasswordLength is deliberately a length floor rather than a composition
// rule: character-class requirements push people toward "Password1!" while
// making long passphrases fail.
const MinPasswordLength = 12

var (
	// ErrInvalidHash means the stored hash is not a hash this code wrote.
	ErrInvalidHash = errors.New("auth: unrecognised password hash")
	// ErrPasswordMismatch means the password did not match.
	ErrPasswordMismatch = errors.New("auth: password does not match")
)

func argonThreads() uint8 {
	if n := runtime.NumCPU(); n < 4 {
		return uint8(max(n, 1))
	}
	return 4
}

// HashPassword returns a PHC-formatted argon2id hash. The parameters are
// encoded in the string so they can be raised later without invalidating
// existing passwords.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	threads := argonThreads()
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, threads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches encoded.
//
// It re-derives with the parameters stored in the hash, not the current
// constants, so old hashes keep verifying after the cost is raised.
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return ErrInvalidHash
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return ErrInvalidHash
	}
	if memory == 0 || time == 0 || threads == 0 {
		return ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return ErrInvalidHash
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

// ValidatePassword checks a new password before it is hashed.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("Password must be at least %d characters.", MinPasswordLength)
	}
	// bcrypt-style truncation is not a concern for argon2, but an unbounded
	// password is an easy way to burn 64 MiB of CPU per request.
	if len(password) > 1024 {
		return errors.New("Password must be at most 1024 characters.")
	}
	return nil
}
