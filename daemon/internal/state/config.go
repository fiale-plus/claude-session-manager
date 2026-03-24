package state

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// Config holds daemon-wide configuration persisted to ~/.csm/config.json.
type Config struct {
	// DefaultAutopilot is the autopilot mode applied to newly discovered
	// sessions that have no persisted per-session override.
	// Valid values: "" (none), "on", "yolo".
	DefaultAutopilot string `json:"default_autopilot"`
}

// configPath returns the path to the config file (~/.csm/config.json).
func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".csm", "config.json")
}

// loadConfig reads ~/.csm/config.json and returns the parsed Config.
// Missing file is treated as empty Config (zero value).
func loadConfig(path string) Config {
	if path == "" {
		return Config{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// File not found is normal on first run.
		return Config{}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("state: failed to parse config %s: %v", path, err)
		return Config{}
	}
	return cfg
}

// saveConfig writes cfg to the given path as JSON.
func saveConfig(path string, cfg Config) {
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Printf("state: failed to marshal config: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("state: failed to save config to %s: %v", path, err)
	}
}
