package auth

import (
	"testing"
	"time"
)

func TestTOTPGenerationAndVerification(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret failed: %v", err)
	}
	if len(secret) == 0 {
		t.Fatal("secret is empty")
	}

	url := GenerateTOTPURL("dockdeploy", "admin@example.com", secret)
	if len(url) == 0 {
		t.Fatal("url is empty")
	}

	now := time.Now()
	code, err := GenerateTOTPCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode failed: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6 digits code, got %s", code)
	}

	// Verify exact time
	if !VerifyTOTP(secret, code, now) {
		t.Fatal("expected TOTP code to verify at current time")
	}

	// Verify -15 seconds (within same or adjacent step)
	if !VerifyTOTP(secret, code, now.Add(-15*time.Second)) {
		t.Fatal("expected TOTP code to verify at -15s")
	}

	// Verify +15 seconds
	if !VerifyTOTP(secret, code, now.Add(15*time.Second)) {
		t.Fatal("expected TOTP code to verify at +15s")
	}

	// Should fail with invalid code
	if VerifyTOTP(secret, "000000", now) && code != "000000" {
		t.Fatal("expected wrong code to fail verification")
	}

	// Should fail with wrong length
	if VerifyTOTP(secret, "123", now) {
		t.Fatal("expected short code to fail")
	}

	// Should fail on far future time (+5 minutes)
	if VerifyTOTP(secret, code, now.Add(5*time.Minute)) {
		t.Fatal("expected code to expire after 5 minutes")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(10)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes failed: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("expected 10 recovery codes, got %d", len(codes))
	}

	first := codes[0]
	normalized := NormalizeRecoveryCode(first)
	if len(normalized) != 8 {
		t.Fatalf("expected normalized length 8, got %s (%d)", normalized, len(normalized))
	}

	// Test case and space insensitivity
	withSpacesAndUpper := " " + codes[0] + " "
	if NormalizeRecoveryCode(withSpacesAndUpper) != normalized {
		t.Fatal("expected NormalizeRecoveryCode to be whitespace insensitive")
	}
}
