package config

import (
	"os"
	"path/filepath"
)

// Fixed list of fallback models
var FallbackModels = []string{
	"qwen3.6-plus",
	"qwen3.5-plus",
	"minimax-m2.7",
	"minimax-m2.5",
}

var (
 PORT          string
 UPSTREAM_BASE string
 UPSTREAM_KEY  string
 DEFAULT_MODEL string
 WITH_FALLBACK bool
 OVERWRITE_MODEL bool
)

func init() {
 PORT = envOrDefault("PORT", "14242")
 UPSTREAM_BASE = envOrDefault("OPENCODE_API_URL", "https://opencode.ai/zen/go/v1")
 UPSTREAM_KEY = os.Getenv("OPENCODE_API_KEY")
 DEFAULT_MODEL = envOrDefault("DEFAULT_MODEL", "qwen3.6-plus")
}

// ConfigDir returns the base directory for config files.
// Resolves to $HOME/.config/ocgo (env: OCGO_CONFIG_DIR overrides).
func ConfigDir() string {
	if d := os.Getenv("OCGO_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "ocgo")
}

// LogDir returns the directory for request logs.
// Resolves to $HOME/.config/ocgo/logs.
func LogDir() string {
	return filepath.Join(ConfigDir(), "logs")
}

func envOrDefault(key, fallback string) string {
 if v := os.Getenv(key); v != "" {
  return v
 }
 return fallback
}