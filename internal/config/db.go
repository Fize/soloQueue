package config

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// ─── LLM Provider CRUD ───────────────────────────────────────────────────────

func LoadProviders(ctx context.Context, db *sqlitedb.DB) ([]LLMProvider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, name, base_url, api_key, api_key_env, enabled, is_default, timeout_ms,
		        max_retries, initial_delay_ms, max_delay_ms, backoff_multiplier, headers
		 FROM llm_providers ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("config db: load providers: %w", err)
	}
	defer rows.Close()

	var providers []LLMProvider
	for rows.Next() {
		var p LLMProvider
		var enabledVal, isDefaultVal int
		var headersVal string
		err := rows.Scan(
			&p.ID, &p.Name, &p.BaseURL, &p.APIKey, &p.APIKeyEnv, &enabledVal, &isDefaultVal, &p.TimeoutMs,
			&p.Retry.MaxRetries, &p.Retry.InitialDelayMs, &p.Retry.MaxDelayMs, &p.Retry.BackoffMultiplier,
			&headersVal,
		)
		if err != nil {
			return nil, fmt.Errorf("config db: scan provider: %w", err)
		}
		p.Enabled = enabledVal != 0
		p.IsDefault = isDefaultVal != 0

		if headersVal != "" {
			_ = json.Unmarshal([]byte(headersVal), &p.Headers)
		}
		if p.Headers == nil {
			p.Headers = make(map[string]string)
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func SaveProvider(ctx context.Context, db *sqlitedb.DB, p LLMProvider) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	headersBytes, err := json.Marshal(p.Headers)
	if err != nil {
		return fmt.Errorf("config db: marshal headers: %w", err)
	}

	enabledVal := 0
	if p.Enabled {
		enabledVal = 1
	}
	isDefaultVal := 0
	if p.IsDefault {
		isDefaultVal = 1
	}

	now := time.Now().Format(time.RFC3339)

	db.WMu.Lock()
	defer db.WMu.Unlock()

	// If is_default is true, unset default status of other providers first
	if p.IsDefault {
		_, err = db.ExecContext(ctx, `UPDATE llm_providers SET is_default = 0`)
		if err != nil {
			return fmt.Errorf("config db: clear existing default provider: %w", err)
		}
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO llm_providers (id, name, base_url, api_key, api_key_env, enabled, is_default, timeout_ms,
		                            max_retries, initial_delay_ms, max_delay_ms, backoff_multiplier, headers,
		                            created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		 	name = excluded.name,
		 	base_url = excluded.base_url,
		 	api_key = excluded.api_key,
		 	api_key_env = excluded.api_key_env,
		 	enabled = excluded.enabled,
		 	is_default = excluded.is_default,
		 	timeout_ms = excluded.timeout_ms,
		 	max_retries = excluded.max_retries,
		 	initial_delay_ms = excluded.initial_delay_ms,
		 	max_delay_ms = excluded.max_delay_ms,
		 	backoff_multiplier = excluded.backoff_multiplier,
		 	headers = excluded.headers,
		 	updated_at = excluded.updated_at`,
		p.ID, p.Name, p.BaseURL, p.APIKey, p.APIKeyEnv, enabledVal, isDefaultVal, p.TimeoutMs,
		p.Retry.MaxRetries, p.Retry.InitialDelayMs, p.Retry.MaxDelayMs, p.Retry.BackoffMultiplier, string(headersBytes),
		now, now,
	)
	if err != nil {
		return fmt.Errorf("config db: save provider %q: %w", p.ID, err)
	}
	return nil
}

func DeleteProvider(ctx context.Context, db *sqlitedb.DB, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	db.WMu.Lock()
	defer db.WMu.Unlock()

	result, err := db.ExecContext(ctx, `DELETE FROM llm_providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("config db: delete provider %q: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("config db: provider %q not found", id)
	}
	return nil
}

// ─── LLM Model CRUD ──────────────────────────────────────────────────────────

func LoadModels(ctx context.Context, db *sqlitedb.DB) ([]LLMModel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, provider_id, name, api_model, context_window, enabled,
		        temperature, max_tokens, thinking_enabled, reasoning_effort
		 FROM llm_models ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("config db: load models: %w", err)
	}
	defer rows.Close()

	var models []LLMModel
	for rows.Next() {
		var m LLMModel
		var enabledVal, thinkingEnabledVal int
		err := rows.Scan(
			&m.ID, &m.ProviderID, &m.Name, &m.APIModel, &m.ContextWindow, &enabledVal,
			&m.Generation.Temperature, &m.Generation.MaxTokens, &thinkingEnabledVal, &m.Thinking.ReasoningEffort,
		)
		if err != nil {
			return nil, fmt.Errorf("config db: scan model: %w", err)
		}
		m.Enabled = enabledVal != 0
		m.Thinking.Enabled = thinkingEnabledVal != 0
		models = append(models, m)
	}
	return models, rows.Err()
}

func SaveModel(ctx context.Context, db *sqlitedb.DB, m LLMModel) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	enabledVal := 0
	if m.Enabled {
		enabledVal = 1
	}
	thinkingEnabledVal := 0
	if m.Thinking.Enabled {
		thinkingEnabledVal = 1
	}

	now := time.Now().Format(time.RFC3339)

	db.WMu.Lock()
	defer db.WMu.Unlock()

	_, err := db.ExecContext(ctx,
		`INSERT INTO llm_models (id, provider_id, name, api_model, context_window, enabled,
		                         temperature, max_tokens, thinking_enabled, reasoning_effort,
		                         created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		 	provider_id = excluded.provider_id,
		 	name = excluded.name,
		 	api_model = excluded.api_model,
		 	context_window = excluded.context_window,
		 	enabled = excluded.enabled,
		 	temperature = excluded.temperature,
		 	max_tokens = excluded.max_tokens,
		 	thinking_enabled = excluded.thinking_enabled,
		 	reasoning_effort = excluded.reasoning_effort,
		 	updated_at = excluded.updated_at`,
		m.ID, m.ProviderID, m.Name, m.APIModel, m.ContextWindow, enabledVal,
		m.Generation.Temperature, m.Generation.MaxTokens, thinkingEnabledVal, m.Thinking.ReasoningEffort,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("config db: save model %q: %w", m.ID, err)
	}
	return nil
}

func DeleteModel(ctx context.Context, db *sqlitedb.DB, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	db.WMu.Lock()
	defer db.WMu.Unlock()

	result, err := db.ExecContext(ctx, `DELETE FROM llm_models WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("config db: delete model %q: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("config db: model %q not found", id)
	}
	return nil
}

// ─── Default Models CRUD ─────────────────────────────────────────────────────

func LoadDefaultModels(ctx context.Context, db *sqlitedb.DB) (DefaultModelsConfig, error) {
	var c DefaultModelsConfig
	if err := ctx.Err(); err != nil {
		return c, err
	}

	rows, err := db.QueryContext(ctx, `SELECT role, model_ref FROM llm_default_models`)
	if err != nil {
		return c, fmt.Errorf("config db: load default models: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var role, modelRef string
		if err := rows.Scan(&role, &modelRef); err != nil {
			return c, fmt.Errorf("config db: scan default model: %w", err)
		}
		switch role {
		case "expert":
			c.Expert = modelRef
		case "superior":
			c.Superior = modelRef
		case "universal":
			c.Universal = modelRef
		case "fast":
			c.Fast = modelRef
		case "fallback":
			c.Fallback = modelRef
		}
	}
	return c, rows.Err()
}

func SaveDefaultModels(ctx context.Context, db *sqlitedb.DB, c DefaultModelsConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	roles := map[string]string{
		"expert":    c.Expert,
		"superior":  c.Superior,
		"universal": c.Universal,
		"fast":      c.Fast,
		"fallback":  c.Fallback,
	}

	now := time.Now().Format(time.RFC3339)

	db.WMu.Lock()
	defer db.WMu.Unlock()

	for role, modelRef := range roles {
		// Only save non-empty references, or delete if empty
		if modelRef == "" {
			_, err := db.ExecContext(ctx, `DELETE FROM llm_default_models WHERE role = ?`, role)
			if err != nil {
				return fmt.Errorf("config db: delete default model for role %q: %w", role, err)
			}
			continue
		}

		_, err := db.ExecContext(ctx,
			`INSERT INTO llm_default_models (role, model_ref, updated_at)
			 VALUES (?, ?, ?)
			 ON CONFLICT(role) DO UPDATE SET
			 	model_ref = excluded.model_ref,
			 	updated_at = excluded.updated_at`,
			role, modelRef, now,
		)
		if err != nil {
			return fmt.Errorf("config db: save default model for role %q: %w", role, err)
		}
	}

	return nil
}
