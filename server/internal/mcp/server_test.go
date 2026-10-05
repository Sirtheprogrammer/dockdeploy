package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMCPServerToolsList(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	ctx := context.Background()

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := srv.Handle(ctx, req)
	if resp.Error != nil {
		t.Fatalf("expected no error, got: %v", resp.Error)
	}

	resMap, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got: %T", resp.Result)
	}

	tools, ok := resMap["tools"].([]map[string]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected tools list to have entries, got: %v", resMap)
	}

	t.Logf("Found %d tools in MCP server:", len(tools))
	for _, tool := range tools {
		t.Logf("- %s: %s", tool["name"], tool["description"])
	}
}

func TestMCPInitialize(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	ctx := context.Background()

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "init-1",
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2024-11-05"}`),
	}

	resp := srv.Handle(ctx, req)
	if resp.Error != nil {
		t.Fatalf("expected no error, got: %v", resp.Error)
	}

	resMap, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got: %T", resp.Result)
	}

	if resMap["protocolVersion"] != "2024-11-05" {
		t.Errorf("unexpected protocolVersion: %v", resMap["protocolVersion"])
	}
}
