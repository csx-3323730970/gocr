package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Provider ProviderConfig `yaml:"provider"`
	Review   ReviewConfig   `yaml:"review"`
	Server   ServerConfig   `yaml:"server"`
}

type ProviderConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	RPM     int    `yaml:"rpm"`
}

type ReviewConfig struct {
	Concurrency  int   `yaml:"concurrency"`
	MaxRounds    int   `yaml:"max_rounds"`
	TokenBudget  int   `yaml:"token_budget"`
	MaxFileBytes int64 `yaml:"max_file_bytes"`
	BundleSize   int   `yaml:"bundle_size"`
}

type ServerConfig struct {
	Addr     string `yaml:"addr"`
	DSN      string `yaml:"dsn"`
	Workers  int    `yaml:"workers"`
	MaxQueue int    `yaml:"max_queue"`
}

func Default() Config {
	return Config{
		Provider: ProviderConfig{
			BaseURL: "https://api.deepseek.com",
			Model:   "deepseek-chat",
			RPM:     60,
		},
		Review: ReviewConfig{
			Concurrency:  4,
			MaxRounds:    6,
			TokenBudget:  200_000,
			MaxFileBytes: 1 << 20,
			BundleSize:   5,
		},
		Server: ServerConfig{
			Addr:     ":8080",
			Workers:  2,
			MaxQueue: 100,
		},
	}
}

// Load reads the config file (defaults to .gocr.yaml) and applies env overrides.
// Precedence: defaults < file < environment. CLI flags are applied by callers.
func Load(path string) (Config, error) {
	cfg := Default()

	if path == "" {
		path = ".gocr.yaml"
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			applyEnv(&cfg)
			return cfg, nil
		}
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	applyEnv(&cfg)
	return cfg, nil
}

func applyEnv(c *Config) {
	if v := os.Getenv("GOCR_BASE_URL"); v != "" {
		c.Provider.BaseURL = v
	}
	if v := os.Getenv("GOCR_API_KEY"); v != "" {
		c.Provider.APIKey = v
	}
	if v := os.Getenv("GOCR_MODEL"); v != "" {
		c.Provider.Model = v
	}
	if v := os.Getenv("GOCR_DSN"); v != "" {
		c.Server.DSN = v
	}
	if v := os.Getenv("GOCR_ADDR"); v != "" {
		c.Server.Addr = v
	}
	envInt("GOCR_RPM", &c.Provider.RPM)
	envInt("GOCR_CONCURRENCY", &c.Review.Concurrency)
	envInt("GOCR_MAX_ROUNDS", &c.Review.MaxRounds)
	envInt("GOCR_TOKEN_BUDGET", &c.Review.TokenBudget)
	envInt64("GOCR_MAX_FILE_BYTES", &c.Review.MaxFileBytes)
	envInt("GOCR_BUNDLE_SIZE", &c.Review.BundleSize)
	envInt("GOCR_WORKERS", &c.Server.Workers)
	envInt("GOCR_MAX_QUEUE", &c.Server.MaxQueue)
}

func envInt(key string, dst *int) {
	v := os.Getenv(key)
	if v == "" {
		return
	}
	if n, err := strconv.Atoi(v); err == nil {
		*dst = n
	}
}

func envInt64(key string, dst *int64) {
	v := os.Getenv(key)
	if v == "" {
		return
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		*dst = n
	}
}
