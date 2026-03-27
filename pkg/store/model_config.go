package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ModelConfig represents a model configuration row.
type ModelConfig struct {
	Role      string
	Provider  string
	Model     string
	Metadata  string // raw JSON
	UpdatedAt string
}

// GetModelConfig returns the model config for a given role.
func (s *Store) GetModelConfig(ctx context.Context, role string) (*ModelConfig, error) {
	var mc ModelConfig
	err := s.db.QueryRowContext(ctx,
		`SELECT role, provider, model, metadata, updated_at
		 FROM model_config WHERE role = ?`, role,
	).Scan(&mc.Role, &mc.Provider, &mc.Model, &mc.Metadata, &mc.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("model config role=%q: %w", role, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting model config role=%q: %w", role, err)
	}
	return &mc, nil
}

// SetModelConfig upserts a model configuration row.
func (s *Store) SetModelConfig(ctx context.Context, cfg ModelConfig) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO model_config (role, provider, model, metadata, updated_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		cfg.Role, cfg.Provider, cfg.Model, cfg.Metadata,
	)
	if err != nil {
		return fmt.Errorf("setting model config role=%q: %w", cfg.Role, err)
	}
	return nil
}

// ListModelConfigs returns all model configuration rows.
// Returns an empty slice (not nil) if no configs exist.
func (s *Store) ListModelConfigs(ctx context.Context) ([]ModelConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT role, provider, model, metadata, updated_at FROM model_config ORDER BY role`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing model configs: %w", err)
	}
	defer rows.Close()

	configs := []ModelConfig{}
	for rows.Next() {
		var mc ModelConfig
		if err := rows.Scan(&mc.Role, &mc.Provider, &mc.Model, &mc.Metadata, &mc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning model config: %w", err)
		}
		configs = append(configs, mc)
	}
	return configs, rows.Err()
}
