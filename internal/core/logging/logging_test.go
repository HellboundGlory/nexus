package logging

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewLogsAtInfo(t *testing.T) {
	var buf bytes.Buffer
	log := New("info", &buf)
	log.Info("hello", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line not JSON: %v", err)
	}
	if rec["msg"] != "hello" || rec["k"] != "v" {
		t.Fatalf("unexpected record: %v", rec)
	}
}

func TestDebugSuppressedAtInfo(t *testing.T) {
	var buf bytes.Buffer
	log := New("info", &buf)
	log.Debug("secret")
	if buf.Len() != 0 {
		t.Fatalf("debug should be suppressed at info level, got %q", buf.String())
	}
}
