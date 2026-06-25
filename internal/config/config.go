package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/skillpath"
)

// flexBool unmarshals JSON booleans, numbers, and strings into a bool.
// Accepted truthy values: true, 1, "1", "true", "yes".
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true", "1", `"1"`, `"true"`, `"yes"`:
		*b = true
	default:
		*b = false
	}
	return nil
}

// Config holds resolved configuration from config.json and env var overrides.
type Config struct {
	AnthropicAPIKeyFile    string   `json:"anthropic_api_key_file"`
	IntegrationTestSession string   `json:"integration_test_session"`
	NoUsage                flexBool `json:"no_usage"`

	anthropicAPIKey string
}

func (c Config) AnthropicAPIKey() string { return c.anthropicAPIKey }

var (
	once     sync.Once
	instance Config
)

// Get returns the singleton Config, loading it on first call.
func Get() Config {
	once.Do(func() {
		dir, err := skillpath.SkillDir()
		if err == nil {
			instance = LoadFromPath(filepath.Join(dir, "config.json"))
		}
	})
	return instance
}

// Reset clears the singleton so the next Get() reloads from disk.
// Intended for tests only.
func Reset() {
	once = sync.Once{}
}

// LoadFromPath reads config.json from the given path and applies env var overrides.
// Missing or malformed config.json returns a zero Config.
func LoadFromPath(path string) Config {
	var cfg Config

	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
	}

	if len(cfg.AnthropicAPIKeyFile) > 0 && cfg.AnthropicAPIKeyFile[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			cfg.AnthropicAPIKeyFile = filepath.Join(home, cfg.AnthropicAPIKeyFile[1:])
		}
	}

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.anthropicAPIKey = key
	}

	if val, ok := os.LookupEnv("CC_SESSION_NO_USAGE"); ok && val != "" {
		cfg.NoUsage = true
	}

	return cfg
}
