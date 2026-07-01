package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
)

type Config struct {
	DataDir  string
	Host     string
	Port     int
	URLBase  string
	LogLevel string
	APIKey   string
}

func (c *Config) Addr() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

// Load builds Config from environment (via getenv), falling back to defaults.
func Load(getenv func(string) string) (*Config, error) {
	c := &Config{
		DataDir:  "./data",
		Host:     "0.0.0.0",
		Port:     9494,
		URLBase:  "",
		LogLevel: "info",
	}
	if v := getenv("NEXUS_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := getenv("NEXUS_HOST"); v != "" {
		c.Host = v
	}
	if v := getenv("NEXUS_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid NEXUS_PORT %q: %w", v, err)
		}
		c.Port = p
	}
	if v := getenv("NEXUS_URL_BASE"); v != "" {
		c.URLBase = v
	}
	if v := getenv("NEXUS_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := getenv("NEXUS_API_KEY"); v != "" {
		c.APIKey = v
	} else {
		key, err := generateAPIKey()
		if err != nil {
			return nil, err
		}
		c.APIKey = key
	}
	return c, nil
}

func generateAPIKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
