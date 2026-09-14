-- +goose Up

CREATE TABLE ai_settings (
    user_id              uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    provider             text NOT NULL DEFAULT 'openai',
    model                text NOT NULL DEFAULT 'gpt-4o',
    base_url             text NOT NULL DEFAULT '',
    api_key_secret_id    uuid REFERENCES secrets(id) ON DELETE SET NULL,
    temperature          double precision NOT NULL DEFAULT 0.7,
    system_prompt_custom text NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER ai_settings_set_updated_at
    BEFORE UPDATE ON ai_settings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE ai_conversations (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      text NOT NULL DEFAULT 'New Chat',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ai_conversations_user_id_idx ON ai_conversations (user_id);

CREATE TRIGGER ai_conversations_set_updated_at
    BEFORE UPDATE ON ai_conversations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE ai_messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    role            text NOT NULL,
    content         text NOT NULL,
    metadata        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ai_messages_conversation_id_idx ON ai_messages (conversation_id, created_at ASC);

-- +goose Down

DROP TABLE IF EXISTS ai_messages;
DROP TABLE IF EXISTS ai_conversations;
DROP TABLE IF EXISTS ai_settings;
