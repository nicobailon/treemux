package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadYAMLConfig(t *testing.T) {
	tmp := t.TempDir()
	confDir := filepath.Join(tmp, "treemux")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgPath := filepath.Join(confDir, "config.yaml")
	content := []byte(`base_branch: develop
path_pattern: subdirectory
session_name: branch
theme: custom`)
	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", tmp)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.BaseBranch != "develop" {
		t.Fatalf("base_branch mismatch: %s", cfg.BaseBranch)
	}
	if cfg.PathPattern != "subdirectory" {
		t.Fatalf("path_pattern mismatch: %s", cfg.PathPattern)
	}
	if cfg.SessionName != "branch" {
		t.Fatalf("session_name mismatch: %s", cfg.SessionName)
	}
	if cfg.Theme != "custom" {
		t.Fatalf("theme mismatch: %s", cfg.Theme)
	}
}
