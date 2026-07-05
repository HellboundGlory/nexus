package automation

import (
	"context"
	"encoding/json"
)

const configSettingKey = "automation.config"

// Config controls the scheduled missing-item sweep. The interval is read at
// startup to register the scheduler; a change takes effect on next startup.
type Config struct {
	MissingSearchIntervalHours int `json:"missingSearchIntervalHours"`
	MissingSearchBatchSize     int `json:"missingSearchBatchSize"`
}

// DefaultConfig is applied when no config has been saved. Deliberately
// conservative because RSS sync (5b) does not exist yet.
func DefaultConfig() Config {
	return Config{MissingSearchIntervalHours: 6, MissingSearchBatchSize: 100}
}

// Config returns the persisted config, or DefaultConfig if none is saved. Any
// non-positive field is replaced with its default so a bad value can't disable
// the sweep or make it unbounded.
func (s *Service) Config(ctx context.Context) (Config, error) {
	raw, ok, err := s.store.GetSetting(ctx, configSettingKey)
	if err != nil {
		return Config{}, err
	}
	if !ok {
		return DefaultConfig(), nil
	}
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Config{}, err
	}
	d := DefaultConfig()
	if c.MissingSearchIntervalHours <= 0 {
		c.MissingSearchIntervalHours = d.MissingSearchIntervalHours
	}
	if c.MissingSearchBatchSize <= 0 {
		c.MissingSearchBatchSize = d.MissingSearchBatchSize
	}
	return c, nil
}

// SetConfig persists the sweep config.
func (s *Service) SetConfig(ctx context.Context, c Config) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, configSettingKey, string(b))
}
