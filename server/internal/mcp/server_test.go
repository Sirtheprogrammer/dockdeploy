package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		name := tool["name"].(string)
		toolNames[name] = true
	}

	expectedTools := []string{
		"dockdeploy_list_servers",
		"dockdeploy_get_server",
		"dockdeploy_exec_command",
		"dockdeploy_docker_exec",
		"dockdeploy_list_containers",
		"dockdeploy_inspect_container",
		"dockdeploy_container_action",
		"dockdeploy_get_container_logs",
		"dockdeploy_restart_container",
		"dockdeploy_read_file",
		"dockdeploy_write_file",
		"dockdeploy_list_files",
		"dockdeploy_list_deployments",
		"dockdeploy_trigger_deployment",
		"dockdeploy_get_deployment_logs",
		"dockdeploy_server_diagnostics",
		"dockdeploy_security_audit",
	}

	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("missing expected tool: %s", expected)
		}
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

	caps, ok := resMap["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("expected capabilities object")
	}
	if _, ok := caps["tools"]; !ok {
		t.Errorf("capabilities missing 'tools'")
	}
	if _, ok := caps["resources"]; !ok {
		t.Errorf("capabilities missing 'resources'")
	}
	if _, ok := caps["prompts"]; !ok {
		t.Errorf("capabilities missing 'prompts'")
	}
}

func TestMCPPing(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	ctx := context.Background()

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "ping-123",
		Method:  "ping",
	}

	resp := srv.Handle(ctx, req)
	if resp.Error != nil {
		t.Fatalf("expected no error, got: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatalf("expected non-nil result")
	}
}

func TestMCPResourcesList(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	ctx := context.Background()

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "resources/list",
	}

	resp := srv.Handle(ctx, req)
	if resp.Error != nil {
		t.Fatalf("expected no error, got: %v", resp.Error)
	}

	resMap, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result")
	}

	resources, ok := resMap["resources"].([]map[string]any)
	if !ok || len(resources) < 2 {
		t.Fatalf("expected at least 2 resources, got: %v", resources)
	}
}

func TestMCPPromptsListAndGet(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	ctx := context.Background()

	reqList := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "prompts/list",
	}

	respList := srv.Handle(ctx, reqList)
	if respList.Error != nil {
		t.Fatalf("expected no error, got: %v", respList.Error)
	}

	reqGet := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "prompts/get",
		Params:  json.RawMessage(`{"name": "troubleshoot_container", "arguments": {"container_id": "test-box"}}`),
	}

	respGet := srv.Handle(ctx, reqGet)
	if respGet.Error != nil {
		t.Fatalf("expected no error, got: %v", respGet.Error)
	}

	resMap, ok := respGet.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result")
	}
	messages, ok := resMap["messages"].([]map[string]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("expected messages in prompt get")
	}
}

func TestMCPHTTPHandler(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	handler := srv.HTTPHandler()

	// 1. Test GET SSE endpoint
	reqGet := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	reqGet.Header.Set("Accept", "text/event-stream")
	recGet := httptest.NewRecorder()
	handler.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200, got: %d", recGet.Code)
	}
	if !strings.Contains(recGet.Body.String(), "event: endpoint") {
		t.Errorf("expected SSE endpoint event, got: %s", recGet.Body.String())
	}

	// 2. Test POST JSON-RPC single request
	body := `{"jsonrpc":"2.0","id":10,"method":"ping"}`
	reqPost := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	recPost := httptest.NewRecorder()
	handler.ServeHTTP(recPost, reqPost)

	if recPost.Code != http.StatusOK {
		t.Fatalf("expected 200, got: %d", recPost.Code)
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(recPost.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}

	// 3. Test POST JSON-RPC batch request
	batchBody := `[{"jsonrpc":"2.0","id":11,"method":"ping"},{"jsonrpc":"2.0","id":12,"method":"ping"}]`
	reqBatch := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(batchBody))
	recBatch := httptest.NewRecorder()
	handler.ServeHTTP(recBatch, reqBatch)

	if recBatch.Code != http.StatusOK {
		t.Fatalf("expected 200, got: %d", recBatch.Code)
	}
	var batchResp []JSONRPCResponse
	if err := json.Unmarshal(recBatch.Body.Bytes(), &batchResp); err != nil {
		t.Fatalf("failed to decode batch response: %v", err)
	}
	if len(batchResp) != 2 {
		t.Errorf("expected 2 batch responses, got: %d", len(batchResp))
	}
}
