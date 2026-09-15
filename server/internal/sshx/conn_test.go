package sshx

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalConnOperations(t *testing.T) {
	ctx := context.Background()
	conn := &Conn{isLocal: true}

	// Test Run
	res, err := conn.Run(ctx, "echo 'hello from local'")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !res.Ok() {
		t.Fatalf("Run exited with %d: %s", res.ExitCode, res.Stderr)
	}
	if res.Output() != "hello from local" {
		t.Fatalf("unexpected output: %q", res.Output())
	}

	// Test Stream
	var stdout, stderr bytes.Buffer
	code, err := conn.Stream(ctx, "echo 'stream test'", &stdout, &stderr)
	if err != nil || code != 0 {
		t.Fatalf("Stream failed: code=%d err=%v", code, err)
	}
	if resStr := stdout.String(); resStr != "stream test\n" {
		t.Fatalf("unexpected stream output: %q", resStr)
	}

	// Test WriteFile and ReadFile
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "nested", "test.txt")
	content := []byte("secret content 123")

	if err := conn.WriteFile(ctx, testFile, content, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected permissions: %v", info.Mode().Perm())
	}

	readBack, err := conn.ReadFile(ctx, testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !bytes.Equal(readBack, content) {
		t.Fatalf("expected %q, got %q", string(content), string(readBack))
	}
}
