package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	c, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 9494 || c.Host != "0.0.0.0" || c.LogLevel != "info" {
		t.Fatalf("bad defaults: %+v", c)
	}
	if c.APIKey == "" {
		t.Fatal("APIKey should be generated when unset")
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	env := map[string]string{
		"NEXUS_PORT":      "8080",
		"NEXUS_LOG_LEVEL": "debug",
		"NEXUS_API_KEY":   "fixedkey",
	}
	c, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8080 || c.LogLevel != "debug" || c.APIKey != "fixedkey" {
		t.Fatalf("env not applied: %+v", c)
	}
}

func TestLoadRejectsBadPort(t *testing.T) {
	env := map[string]string{"NEXUS_PORT": "notanumber"}
	if _, err := Load(func(k string) string { return env[k] }); err == nil {
		t.Fatal("expected error for non-numeric port")
	}
}
