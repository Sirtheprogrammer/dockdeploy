-- +goose Up

CREATE TABLE ai_settings (
    user_id              TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    provider             TEXT NOT NULL DEFAULT 'openai',
    model                TEXT NOT NULL DEFAULT 'gpt-4o',
    base_url             TEXT NOT NULL DEFAULT '',
    api_key_secret_id    TEXT REFERENCES secrets(id) ON DELETE SET NULL,
    temperature          REAL NOT NULL DEFAULT 0.7,
    system_prompt_custom TEXT NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ai_conversations (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      TEXT NOT NULL DEFAULT 'New Chat',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX ai_conversations_user_id_idx ON ai_conversations (user_id);

CREATE TABLE ai_messages (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,
    content         TEXT NOT NULL,
    metadata        TEXT NOT NULL DEFAULT '{}',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX ai_messages_conversation_id_idx ON ai_messages (conversation_id, created_at ASC);

-- +goose Down

DROP TABLE IF EXISTS ai_messages;
DROP TABLE IF EXISTS ai_conversations;
DROP TABLE IF EXISTS ai_settings;
