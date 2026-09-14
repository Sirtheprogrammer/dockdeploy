package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AISettings struct {
	UserID             string    `db:"user_id"              json:"user_id"`
	Provider           string    `db:"provider"             json:"provider"`
	Model              string    `db:"model"                json:"model"`
	BaseURL            string    `db:"base_url"             json:"base_url"`
	APIKeySecretID     *string   `db:"api_key_secret_id"    json:"-"`
	Temperature        float64   `db:"temperature"          json:"temperature"`
	SystemPromptCustom string    `db:"system_prompt_custom" json:"system_prompt_custom"`
	HasAPIKey          bool      `db:"-"                    json:"has_api_key"`
	CreatedAt          time.Time `db:"created_at"           json:"created_at"`
	UpdatedAt          time.Time `db:"updated_at"           json:"updated_at"`
}

const aiSettingsColumns = `user_id, provider, model, base_url, api_key_secret_id, temperature, system_prompt_custom, created_at, updated_at`

type AIConversation struct {
	ID        string    `db:"id"         json:"id"`
	UserID    string    `db:"user_id"    json:"user_id"`
	Title     string    `db:"title"      json:"title"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

const aiConversationColumns = `id, user_id, title, created_at, updated_at`

type AIMessage struct {
	ID             string          `db:"id"              json:"id"`
	ConversationID string          `db:"conversation_id" json:"conversation_id"`
	Role           string          `db:"role"            json:"role"`
	Content        string          `db:"content"         json:"content"`
	Metadata       json.RawMessage `db:"metadata"        json:"metadata"`
	CreatedAt      time.Time       `db:"created_at"      json:"created_at"`
}

const aiMessageColumns = `id, conversation_id, role, content, metadata, created_at`

func DefaultAISettings(userID string) AISettings {
	return AISettings{
		UserID:             userID,
		Provider:           "openai",
		Model:              "gpt-4o",
		BaseURL:            "",
		Temperature:        0.7,
		SystemPromptCustom: "",
		HasAPIKey:          false,
	}
}

func (s *Store) GetAISettings(ctx context.Context, userID string) (*AISettings, error) {
	if s.sqlite != nil {
		return s.sqlite.GetAISettings(ctx, userID)
	}

	rows, err := s.pool.Query(ctx, `SELECT `+aiSettingsColumns+` FROM ai_settings WHERE user_id = $1`, userID)
	if err != nil {
		return nil, wrap("store: get ai settings", err)
	}
	settings, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[AISettings])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			def := DefaultAISettings(userID)
			return &def, nil
		}
		return nil, wrap("store: get ai settings", err)
	}
	settings.HasAPIKey = settings.APIKeySecretID != nil
	return &settings, nil
}

func (s *Store) UpsertAISettings(
	ctx context.Context,
	sealer Sealer,
	userID, provider, model, baseURL, apiKey, systemPromptCustom string,
	temperature float64,
) (*AISettings, error) {
	if s.sqlite != nil {
		return s.sqlite.UpsertAISettings(ctx, sealer, userID, provider, model, baseURL, apiKey, systemPromptCustom, temperature)
	}

	var updated AISettings
	err := s.tx(ctx, func(tx pgx.Tx) error {
		// Check existing settings
		var existingSecretID *string
		err := tx.QueryRow(ctx, `SELECT api_key_secret_id FROM ai_settings WHERE user_id = $1`, userID).Scan(&existingSecretID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return wrap("store: check existing ai settings", err)
		}

		var secretID *string = existingSecretID
		if apiKey != "" {
			// Seal new key
			newSecretID, err := insertSecret(ctx, tx, sealer, KindAIToken, apiKey)
			if err != nil {
				return err
			}
			secretID = newSecretID

			// Delete old secret if replaced
			if existingSecretID != nil {
				if _, err := tx.Exec(ctx, `DELETE FROM secrets WHERE id = $1`, *existingSecretID); err != nil {
					return wrap("store: delete old ai secret", err)
				}
			}
		}

		rows, err := tx.Query(ctx, `
			INSERT INTO ai_settings (user_id, provider, model, base_url, api_key_secret_id, temperature, system_prompt_custom, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now())
			ON CONFLICT (user_id) DO UPDATE SET
				provider = EXCLUDED.provider,
				model = EXCLUDED.model,
				base_url = EXCLUDED.base_url,
				api_key_secret_id = COALESCE(EXCLUDED.api_key_secret_id, ai_settings.api_key_secret_id),
				temperature = EXCLUDED.temperature,
				system_prompt_custom = EXCLUDED.system_prompt_custom,
				updated_at = now()
			RETURNING `+aiSettingsColumns,
			userID, provider, model, baseURL, secretID, temperature, systemPromptCustom)
		if err != nil {
			return wrap("store: upsert ai settings", err)
		}

		res, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[AISettings])
		if err != nil {
			return wrap("store: upsert ai settings", err)
		}
		updated = res
		return nil
	})

	if err != nil {
		return nil, err
	}
	updated.HasAPIKey = updated.APIKeySecretID != nil
	return &updated, nil
}

func (s *Store) GetAIAPIKey(ctx context.Context, sealer Sealer, userID string) (string, error) {
	if s.sqlite != nil {
		return s.sqlite.GetAIAPIKey(ctx, sealer, userID)
	}

	var secretID *string
	err := s.pool.QueryRow(ctx, `SELECT api_key_secret_id FROM ai_settings WHERE user_id = $1`, userID).Scan(&secretID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", wrap("store: get ai secret id", err)
	}
	if secretID == nil {
		return "", nil
	}
	return s.openSecret(ctx, sealer, secretID)
}

func (s *Store) ListAIConversations(ctx context.Context, userID string) ([]AIConversation, error) {
	if s.sqlite != nil {
		return s.sqlite.ListAIConversations(ctx, userID)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+aiConversationColumns+`
		FROM ai_conversations
		WHERE user_id = $1
		ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, wrap("store: list ai conversations", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[AIConversation])
}

func (s *Store) CreateAIConversation(ctx context.Context, userID, title string) (*AIConversation, error) {
	if s.sqlite != nil {
		return s.sqlite.CreateAIConversation(ctx, userID, title)
	}

	if title == "" {
		title = "New Chat"
	}

	rows, err := s.pool.Query(ctx, `
		INSERT INTO ai_conversations (user_id, title)
		VALUES ($1, $2)
		RETURNING `+aiConversationColumns, userID, title)
	if err != nil {
		return nil, wrap("store: create ai conversation", err)
	}
	conv, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[AIConversation])
	if err != nil {
		return nil, wrap("store: create ai conversation", err)
	}
	return &conv, nil
}

func (s *Store) GetAIConversation(ctx context.Context, conversationID, userID string) (*AIConversation, []AIMessage, error) {
	if s.sqlite != nil {
		return s.sqlite.GetAIConversation(ctx, conversationID, userID)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+aiConversationColumns+`
		FROM ai_conversations
		WHERE id = $1 AND user_id = $2`, conversationID, userID)
	if err != nil {
		return nil, nil, wrap("store: get ai conversation", err)
	}
	conv, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[AIConversation])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, wrap("store: get ai conversation", err)
	}

	msgRows, err := s.pool.Query(ctx, `
		SELECT `+aiMessageColumns+`
		FROM ai_messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC`, conversationID)
	if err != nil {
		return nil, nil, wrap("store: get ai messages", err)
	}
	messages, err := pgx.CollectRows(msgRows, pgx.RowToStructByName[AIMessage])
	if err != nil {
		return nil, nil, wrap("store: get ai messages", err)
	}

	return &conv, messages, nil
}

func (s *Store) DeleteAIConversation(ctx context.Context, conversationID, userID string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteAIConversation(ctx, conversationID, userID)
	}

	tag, err := s.pool.Exec(ctx, `
		DELETE FROM ai_conversations
		WHERE id = $1 AND user_id = $2`, conversationID, userID)
	if err != nil {
		return wrap("store: delete ai conversation", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateAIConversationTitle(ctx context.Context, conversationID, userID, title string) error {
	if s.sqlite != nil {
		return s.sqlite.UpdateAIConversationTitle(ctx, conversationID, userID, title)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE ai_conversations
		SET title = $1, updated_at = now()
		WHERE id = $2 AND user_id = $3`, title, conversationID, userID)
	if err != nil {
		return wrap("store: update ai conversation title", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddAIMessage(ctx context.Context, conversationID, role, content string, metadata json.RawMessage) (*AIMessage, error) {
	if s.sqlite != nil {
		return s.sqlite.AddAIMessage(ctx, conversationID, role, content, metadata)
	}

	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}

	var msg AIMessage
	err := s.tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			INSERT INTO ai_messages (conversation_id, role, content, metadata)
			VALUES ($1, $2, $3, $4)
			RETURNING `+aiMessageColumns, conversationID, role, content, metadata)
		if err != nil {
			return wrap("store: add ai message", err)
		}
		res, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[AIMessage])
		if err != nil {
			return wrap("store: add ai message", err)
		}
		msg = res

		// Bump conversation updated_at
		_, err = tx.Exec(ctx, `UPDATE ai_conversations SET updated_at = now() WHERE id = $1`, conversationID)
		return err
	})

	if err != nil {
		return nil, err
	}
	return &msg, nil
}
