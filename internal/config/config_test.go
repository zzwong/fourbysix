package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.LabelW != 4 || cfg.LabelH != 6 {
		t.Fatalf("label = %vx%v, want 4x6", cfg.LabelW, cfg.LabelH)
	}
	if cfg.DPI != 300 || cfg.Margin != 8 || cfg.Rotation != "auto" {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.Threshold || cfg.OutputSuffix != "_label" {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestUserConfigPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/fbs-xdg")
	if got, want := UserConfigPath(), "/tmp/fbs-xdg/fourbysix/config.yaml"; got != want {
		t.Fatalf("UserConfigPath() = %q, want %q", got, want)
	}
}

func TestLoadMergesPartialConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, "label:\n  width: 2\ndpi: 150\nbogus: true\n")

	cfg, err := Load(path, Defaults())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LabelW != 2 || cfg.LabelH != 6 {
		t.Fatalf("label = %vx%v, want 2x6 (partial label keeps default height)", cfg.LabelW, cfg.LabelH)
	}
	if cfg.DPI != 150 || cfg.Margin != 8 || cfg.Rotation != "auto" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadLaterConfigOverridesEarlier(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.yaml")
	explicit := filepath.Join(dir, "explicit.yaml")
	writeFile(t, user, "label:\n  width: 3\nmargin: 12\n")
	writeFile(t, explicit, "label:\n  width: 2\ndpi: 150\n")

	// The CLI applies user then explicit, so explicit wins where they overlap.
	cfg, err := Load(user, Defaults())
	if err != nil {
		t.Fatalf("Load(user): %v", err)
	}
	cfg, err = Load(explicit, cfg)
	if err != nil {
		t.Fatalf("Load(explicit): %v", err)
	}
	if cfg.LabelW != 2 || cfg.LabelH != 6 || cfg.Margin != 12 || cfg.DPI != 150 {
		t.Fatalf("merged cfg = %+v", cfg)
	}
}

func TestLoadDoesNotMutateInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, "dpi: 72\n")
	base := Defaults()
	if _, err := Load(path, base); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if base.DPI != 300 {
		t.Fatalf("base mutated: %+v", base)
	}
}

func TestInitCreatesAndRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fourbysix", "config.yaml")
	if err := Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created config: %v", err)
	}
	if string(got) != string(exampleConfig) {
		t.Fatal("created config does not match the embedded example")
	}
	if err := Init(path); !errors.Is(err, ErrExists) {
		t.Fatalf("second Init error = %v, want ErrExists", err)
	}
}

// TestEmbeddedExampleMatchesRootCopy guards the two copies of the example
// config (the one users read at the repo root and the one embedded here)
// from drifting apart.
func TestEmbeddedExampleMatchesRootCopy(t *testing.T) {
	root, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("read root config.example.yaml: %v", err)
	}
	if string(root) != string(exampleConfig) {
		t.Fatal("root config.example.yaml and internal/config/config.example.yaml differ")
	}
}
