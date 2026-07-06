package automation

import (
	"context"
	"encoding/json"
)

const configSettingKey = "automation.config"

// Config controls the scheduled missing-item sweep and RSS sync. Intervals are
// read at startup to register the scheduler; a change takes effect on next
// startup.
type Config struct {
	MissingSearchIntervalHours int  `json:"missingSearchIntervalHours"`
	MissingSearchBatchSize     int  `json:"missingSearchBatchSize"`
	RSSSyncEnabled             bool `json:"rssSyncEnabled"`
	RSSSyncIntervalMinutes     int  `json:"rssSyncIntervalMinutes"`
}

// DefaultConfig is applied when no config has been saved.
func DefaultConfig() Config {
	return Config{
		MissingSearchIntervalHours: 6,
		MissingSearchBatchSize:     100,
		RSSSyncEnabled:             true,
		RSSSyncIntervalMinutes:     15,
	}
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
	if c.RSSSyncIntervalMinutes <= 0 {
		c.RSSSyncIntervalMinutes = d.RSSSyncIntervalMinutes
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
