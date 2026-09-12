package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	for _, password := range []string{
		"correct horse battery staple",
		"a-very-long-passphrase-with-symbols-!@#$%^&*()",
		"pässwörd-with-unicode-你好",
	} {
		hash, err := HashPassword(password)
		if err != nil {
			t.Fatalf("HashPassword: %v", err)
		}
		if strings.Contains(hash, password) {
			t.Fatal("hash contains the plaintext password")
		}
		if !strings.HasPrefix(hash, "$argon2id$") {
			t.Fatalf("hash = %q, want an argon2id PHC string", hash)
		}
		if err := VerifyPassword(password, hash); err != nil {
			t.Errorf("VerifyPassword(correct) = %v, want nil", err)
		}
		if err := VerifyPassword(password+"x", hash); !errors.Is(err, ErrPasswordMismatch) {
			t.Errorf("VerifyPassword(wrong) = %v, want ErrPasswordMismatch", err)
		}
	}
}

func TestHashPasswordUsesFreshSalt(t *testing.T) {
	a, _ := HashPassword("same password")
	b, _ := HashPassword("same password")
	if a == b {
		t.Fatal("identical hashes for the same password; the salt is not random")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	valid, err := HashPassword("reference password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	parts := strings.Split(valid, "$")

	cases := map[string]string{
		"empty":            "",
		"plaintext":        "reference password",
		"bcrypt":           "$2a$10$abcdefghijklmnopqrstuv",
		"wrong algorithm":  "$argon2i$v=19$m=65536,t=2,p=4$c2FsdA$aGFzaA",
		"wrong version":    "$argon2id$v=18$m=65536,t=2,p=4$c2FsdA$aGFzaA",
		"missing params":   "$argon2id$v=19$$c2FsdA$aGFzaA",
		"zero memory":      "$argon2id$v=19$m=0,t=2,p=4$c2FsdA$aGFzaA",
		"zero time":        "$argon2id$v=19$m=65536,t=0,p=4$c2FsdA$aGFzaA",
		"zero threads":     "$argon2id$v=19$m=65536,t=2,p=0$c2FsdA$aGFzaA",
		"bad base64 salt":  "$argon2id$v=19$m=65536,t=2,p=4$not!base64$aGFzaA",
		"empty salt":       "$argon2id$v=19$m=65536,t=2,p=4$$aGFzaA",
		"empty key":        "$argon2id$v=19$m=65536,t=2,p=4$c2FsdA$",
		"truncated fields": strings.Join(parts[:5], "$"),
	}

	for name, hash := range cases {
		t.Run(name, func(t *testing.T) {
			if err := VerifyPassword("reference password", hash); !errors.Is(err, ErrInvalidHash) {
				t.Errorf("VerifyPassword = %v, want ErrInvalidHash", err)
			}
		})
	}
}

// A hash written with different cost parameters must keep verifying, so the
// cost can be raised later without locking every existing user out.
func TestVerifyPasswordHonoursEmbeddedParameters(t *testing.T) {
	const password = "migrated password"

	// Stand in for a hash written by an older, cheaper configuration.
	salt := []byte("sixteen-byte-slt")
	key := argon2.IDKey([]byte(password), salt, 1, 8*1024, 1, 32)
	legacy := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, 8*1024, 1, 1,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))

	if err := VerifyPassword(password, legacy); err != nil {
		t.Errorf("VerifyPassword(legacy hash) = %v, want nil", err)
	}
	if err := VerifyPassword("wrong", legacy); !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("VerifyPassword(legacy hash, wrong password) = %v, want ErrPasswordMismatch", err)
	}

	// New hashes must still be written at the current cost.
	fresh, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.Contains(fresh, "m=65536,t=2,p=") {
		t.Errorf("fresh hash = %q, want the current cost parameters recorded", fresh)
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLength-1)); err == nil {
		t.Error("short password accepted")
	}
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLength)); err != nil {
		t.Errorf("password at the minimum length rejected: %v", err)
	}
	// An unbounded password is an easy way to burn 64 MiB of CPU per request.
	if err := ValidatePassword(strings.Repeat("a", 1025)); err == nil {
		t.Error("oversized password accepted")
	}
}
