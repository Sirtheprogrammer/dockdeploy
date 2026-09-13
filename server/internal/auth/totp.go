package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TOTPStep is the time step in seconds as defined by RFC 6238.
const TOTPStep = 30

// TOTPDigits is the number of digits for TOTP codes.
const TOTPDigits = 6

// GenerateTOTPSecret returns a 20-byte cryptographically secure random key
// encoded as an unpadded Base32 string (RFC 4648).
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("totp: generate secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// GenerateTOTPURL builds an otpauth:// URI suitable for scanning into Google
// Authenticator, Authy, Bitwarden, etc.
func GenerateTOTPURL(issuer, accountEmail, secret string) string {
	cleanSecret := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	label := fmt.Sprintf("%s:%s", issuer, accountEmail)
	q := url.Values{}
	q.Set("secret", cleanSecret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(TOTPDigits))
	q.Set("period", strconv.Itoa(TOTPStep))

	return fmt.Sprintf("otpauth://totp/%s?%s", url.PathEscape(label), q.Encode())
}

// VerifyTOTP validates a 6-digit TOTP code against a Base32 secret.
// It checks current time step as well as +/- 1 time step to allow for 30 seconds clock drift.
func VerifyTOTP(secret string, passcode string, now time.Time) bool {
	cleanSecret := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleanSecret)
	if err != nil {
		secretBytes, err = base32.StdEncoding.DecodeString(cleanSecret)
		if err != nil {
			return false
		}
	}

	cleanCode := strings.TrimSpace(passcode)
	if len(cleanCode) != TOTPDigits {
		return false
	}
	num, err := strconv.Atoi(cleanCode)
	if err != nil {
		return false
	}

	currentStep := now.Unix() / TOTPStep
	for _, step := range []int64{currentStep - 1, currentStep, currentStep + 1} {
		if computeCode(secretBytes, step) == num {
			return true
		}
	}
	return false
}

// GenerateTOTPCode generates the TOTP code for a given timestamp (useful for testing and verification).
func GenerateTOTPCode(secret string, t time.Time) (string, error) {
	cleanSecret := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleanSecret)
	if err != nil {
		secretBytes, err = base32.StdEncoding.DecodeString(cleanSecret)
		if err != nil {
			return "", errors.New("invalid base32 secret")
		}
	}
	step := t.Unix() / TOTPStep
	code := computeCode(secretBytes, step)
	return fmt.Sprintf("%06d", code), nil
}

func computeCode(secret []byte, counter int64) int {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	binaryCode := (binary.BigEndian.Uint32(h[offset:offset+4]) & 0x7fffffff) % 1000000
	return int(binaryCode)
}

// GenerateRecoveryCodes generates count recovery codes in the format "xxxx-xxxx".
func GenerateRecoveryCodes(count int) ([]string, error) {
	if count <= 0 {
		count = 10
	}
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("totp: generate recovery code: %w", err)
		}
		hexStr := hex.EncodeToString(b)
		codes[i] = fmt.Sprintf("%s-%s", hexStr[:4], hexStr[4:])
	}
	return codes, nil
}

// NormalizeRecoveryCode cleans up a user-supplied recovery code for comparison.
func NormalizeRecoveryCode(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	c = strings.ReplaceAll(c, "-", "")
	c = strings.ReplaceAll(c, " ", "")
	return c
}
