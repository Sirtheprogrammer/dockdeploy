package secrets

import (
	"bytes"
	"errors"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestNewSealerRejectsBadKeyLength(t *testing.T) {
	for _, size := range []int{0, 1, 16, 24, 31, 33} {
		if _, err := NewSealer(make([]byte, size)); err == nil {
			t.Errorf("NewSealer(%d bytes) = nil error, want error", size)
		}
	}
	if _, err := NewSealer(make([]byte, 32)); err != nil {
		t.Errorf("NewSealer(32 bytes) = %v, want nil", err)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	s, err := NewSealer(testKey(t))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	cases := []struct {
		name      string
		plaintext string
		aad       string
	}{
		{"empty", "", "row-1"},
		{"short", "hunter2", "row-1"},
		{"no aad", "hunter2", ""},
		{"private key", "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----", "row-2"},
		{"unicode", "pässwörd-\u4f60\u597d", "row-3"},
		{"long", string(bytes.Repeat([]byte("x"), 64*1024)), "row-4"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nonce, ciphertext, err := s.SealString(tc.plaintext, []byte(tc.aad))
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			if tc.plaintext != "" && bytes.Contains(ciphertext, []byte(tc.plaintext)) {
				t.Fatal("ciphertext contains the plaintext")
			}
			got, err := s.OpenString(nonce, ciphertext, []byte(tc.aad))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if got != tc.plaintext {
				t.Errorf("round trip = %q, want %q", got, tc.plaintext)
			}
		})
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	s, _ := NewSealer(testKey(t))
	seen := make(map[string]bool, 128)
	for i := 0; i < 128; i++ {
		nonce, ciphertext, err := s.Seal([]byte("same plaintext"), []byte("same aad"))
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		if seen[string(nonce)] {
			t.Fatal("nonce reused; GCM is catastrophically broken by nonce reuse")
		}
		seen[string(nonce)] = true
		if seen[string(ciphertext)] {
			t.Fatal("identical ciphertext for repeated plaintext")
		}
		seen[string(ciphertext)] = true
	}
}

func TestOpenRejectsTampering(t *testing.T) {
	s, _ := NewSealer(testKey(t))
	nonce, ciphertext, err := s.Seal([]byte("ssh-private-key"), []byte("secret-uuid-a"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	corrupt := func(b []byte) []byte {
		out := bytes.Clone(b)
		out[0] ^= 0xff
		return out
	}

	cases := []struct {
		name  string
		nonce []byte
		ct    []byte
		aad   []byte
	}{
		{"flipped ciphertext bit", nonce, corrupt(ciphertext), []byte("secret-uuid-a")},
		{"flipped nonce bit", corrupt(nonce), ciphertext, []byte("secret-uuid-a")},
		// The important one: a ciphertext lifted onto a different row.
		{"aad mismatch", nonce, ciphertext, []byte("secret-uuid-b")},
		{"missing aad", nonce, ciphertext, nil},
		{"truncated ciphertext", nonce, ciphertext[:len(ciphertext)-1], []byte("secret-uuid-a")},
		{"empty ciphertext", nonce, nil, []byte("secret-uuid-a")},
		{"short nonce", nonce[:len(nonce)-1], ciphertext, []byte("secret-uuid-a")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Open(tc.nonce, tc.ct, tc.aad); !errors.Is(err, ErrTampered) {
				t.Errorf("Open = %v, want ErrTampered", err)
			}
		})
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	a, _ := NewSealer(testKey(t))
	other := testKey(t)
	other[0] ^= 0xff
	b, _ := NewSealer(other)

	nonce, ciphertext, err := a.Seal([]byte("registry-password"), []byte("row"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := b.Open(nonce, ciphertext, []byte("row")); !errors.Is(err, ErrTampered) {
		t.Errorf("Open with wrong key = %v, want ErrTampered", err)
	}
}
