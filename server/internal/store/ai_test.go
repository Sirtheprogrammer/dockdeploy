package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/secrets"
)

func TestSQLiteStore_AIAssistant(t *testing.T) {
	ctx := context.Background()
	st, _ := setupTestSQLiteStore(t)

	key := make([]byte, 32)
	copy(key, []byte("01234567890123456789012345678901"))
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatalf("NewSealer failed: %v", err)
	}

	user, err := st.CreateFirstAdmin(ctx, "ai@example.com", "AI User", "hash")
	if err != nil {
		t.Fatalf("CreateFirstAdmin failed: %v", err)
	}

	// 1. Default settings when none stored
	settings, err := st.GetAISettings(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetAISettings default failed: %v", err)
	}
	if settings.Provider != "openai" || settings.HasAPIKey {
		t.Errorf("unexpected default settings: %+v", settings)
	}

	// 2. Upsert settings with API key
	saved, err := st.UpsertAISettings(ctx, sealer, user.ID, "anthropic", "claude-3-7-sonnet", "https://api.anthropic.com", "sk-ant-test-key", "You are helpful", 0.5)
	if err != nil {
		t.Fatalf("UpsertAISettings failed: %v", err)
	}
	if saved.Provider != "anthropic" || !saved.HasAPIKey || saved.Model != "claude-3-7-sonnet" {
		t.Errorf("unexpected saved settings: %+v", saved)
	}

	// 3. Read back API key
	apiKey, err := st.GetAIAPIKey(ctx, sealer, user.ID)
	if err != nil {
		t.Fatalf("GetAIAPIKey failed: %v", err)
	}
	if apiKey != "sk-ant-test-key" {
		t.Errorf("expected sk-ant-test-key, got %s", apiKey)
	}

	// 4. Update settings without providing key (should retain key)
	saved2, err := st.UpsertAISettings(ctx, sealer, user.ID, "deepseek", "deepseek-chat", "", "", "Custom instructions", 0.8)
	if err != nil {
		t.Fatalf("UpsertAISettings without key failed: %v", err)
	}
	if saved2.Provider != "deepseek" || !saved2.HasAPIKey {
		t.Errorf("expected retained key, got %+v", saved2)
	}
	apiKey2, err := st.GetAIAPIKey(ctx, sealer, user.ID)
	if err != nil || apiKey2 != "sk-ant-test-key" {
		t.Errorf("expected key to be retained, got %s, err: %v", apiKey2, err)
	}

	// 5. Conversations and Messages
	conv, err := st.CreateAIConversation(ctx, user.ID, "Server Diagnostic Chat")
	if err != nil {
		t.Fatalf("CreateAIConversation failed: %v", err)
	}
	if conv.Title != "Server Diagnostic Chat" {
		t.Errorf("unexpected conv title: %s", conv.Title)
	}

	convs, err := st.ListAIConversations(ctx, user.ID)
	if err != nil || len(convs) != 1 {
		t.Fatalf("ListAIConversations failed, len: %d, err: %v", len(convs), err)
	}

	// Add messages
	m1, err := st.AddAIMessage(ctx, conv.ID, "user", "Diagnose server srv-1", json.RawMessage(`{"server_id":"srv-1"}`))
	if err != nil {
		t.Fatalf("AddAIMessage 1 failed: %v", err)
	}
	if m1.Role != "user" || m1.Content != "Diagnose server srv-1" {
		t.Errorf("unexpected m1: %+v", m1)
	}

	m2, err := st.AddAIMessage(ctx, conv.ID, "assistant", "Server CPU is 12%, Memory 45%. All containers healthy.", nil)
	if err != nil {
		t.Fatalf("AddAIMessage 2 failed: %v", err)
	}
	if m2.Role != "assistant" {
		t.Errorf("unexpected m2: %+v", m2)
	}

	readConv, msgs, err := st.GetAIConversation(ctx, conv.ID, user.ID)
	if err != nil {
		t.Fatalf("GetAIConversation failed: %v", err)
	}
	if readConv.ID != conv.ID || len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Rename conversation
	err = st.UpdateAIConversationTitle(ctx, conv.ID, user.ID, "Updated Title")
	if err != nil {
		t.Fatalf("UpdateAIConversationTitle failed: %v", err)
	}

	// Delete conversation
	err = st.DeleteAIConversation(ctx, conv.ID, user.ID)
	if err != nil {
		t.Fatalf("DeleteAIConversation failed: %v", err)
	}

	convsAfter, err := st.ListAIConversations(ctx, user.ID)
	if err != nil || len(convsAfter) != 0 {
		t.Fatalf("expected 0 conversations after delete, got %d", len(convsAfter))
	}
}
