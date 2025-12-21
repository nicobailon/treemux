package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

const (
	defaultBaseBranch  = "main"
	defaultPathPattern = "sibling"
	defaultSessionName = "folder"
	defaultTheme       = "catppuccin-mocha"
)

type Config struct {
	BaseBranch  string `mapstructure:"base_branch"`
	PathPattern string `mapstructure:"path_pattern"`
	SessionName string `mapstructure:"session_name"`
	Theme       string `mapstructure:"theme"`
}

func defaultConfig() *Config {
	return &Config{
		BaseBranch:  defaultBaseBranch,
		PathPattern: defaultPathPattern,
		SessionName: defaultSessionName,
		Theme:       defaultTheme,
	}
}

func Load() (*Config, error) {
	cfg := defaultConfig()

	v := viper.New()
	v.SetConfigName("config")
	v.AddConfigPath(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "treemux"))
	v.AddConfigPath(filepath.Join(os.Getenv("HOME"), ".config", "treemux"))
	v.SetConfigType("yaml")

	v.SetDefault("base_branch", defaultBaseBranch)
	v.SetDefault("path_pattern", defaultPathPattern)
	v.SetDefault("session_name", defaultSessionName)
	v.SetDefault("theme", defaultTheme)

	if err := v.ReadInConfig(); err == nil {
		if err := v.Unmarshal(&cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	// fallback to TOML if yaml missing
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err == nil {
		if err := v.Unmarshal(&cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	// legacy shell config
	if legacyCfg, err := loadLegacy(); err == nil && legacyCfg != nil {
		return legacyCfg, nil
	}

	return cfg, nil
}

func loadLegacy() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".config", "treemux", "config")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := defaultConfig()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := strings.Trim(parts[1], "\"'")
		switch key {
		case "TREEMUX_BASE_BRANCH":
			cfg.BaseBranch = val
		case "TREEMUX_PATH_PATTERN":
			cfg.PathPattern = val
		case "TREEMUX_SESSION_NAME":
			cfg.SessionName = val
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// Detect whether file had any relevant keys; if not, return error to fall back to defaults.
	if cfg.BaseBranch == defaultBaseBranch && cfg.PathPattern == defaultPathPattern && cfg.SessionName == defaultSessionName {
		return nil, errors.New("no legacy keys")
	}
	return cfg, nil
}
