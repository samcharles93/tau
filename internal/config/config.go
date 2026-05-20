package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds user preferences persisted to ~/.config/aim/config.yaml.
type Config struct {
	DefaultEndpoint string `yaml:"default_endpoint"`
	DefaultModel    string `yaml:"default_model"`
	TokenExpiry     string `yaml:"token_expiry"`
	SkipTLSVerify   bool   `yaml:"skip_tls_verify"`
}

func defaultConfig() Config {
	return Config{
		DefaultEndpoint: "rcc/npr",
		DefaultModel:    "nemotron-nano-9b",
		TokenExpiry:     "8h",
		SkipTLSVerify:   false,
	}
}

func configDir() string {
	if dir := os.Getenv("AIM_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "aim")
}

func configPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

func LoadConfig() Config {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	_ = yaml.Unmarshal(data, &cfg)
	return cfg
}

// Dir returns the AIM configuration directory.
func Dir() string {
	return configDir()
}

// TODO saveConfig should be called whenever the user changes a preference.
/*
	Any saves should be atomic, they should write to a temp file, and then rename to avoid corruption on crashes.
	The `/reload` command should re-read the config file and update the in-memory config,
	so that changes can be made on the fly without a restart.
*/
// func saveConfig(cfg Config) error {
// 	dir := configDir()
// 	if err := os.MkdirAll(dir, 0o700); err != nil {
// 		return err
// 	}
// 	data, err := yaml.Marshal(cfg)
// 	if err != nil {
// 		return err
// 	}
// 	return os.WriteFile(configPath(), data, 0o600)
// }
