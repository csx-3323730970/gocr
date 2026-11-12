package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileThenEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gocr.yaml")
	if err := os.WriteFile(path, []byte("provider:\n  model: from-file\nreview:\n  concurrency: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOCR_MODEL", "from-env")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "from-env" {
		t.Fatalf("model = %q, want env override", cfg.Provider.Model)
	}
	if cfg.Review.Concurrency != 2 {
		t.Fatalf("concurrency = %d, want 2 from file", cfg.Review.Concurrency)
	}
	if cfg.Server.Addr != ":8080" {
		t.Fatalf("addr = %q, want default", cfg.Server.Addr)
	}
}

func TestLoadMissingExplicitFileFails(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing explicit config file")
	}
}
