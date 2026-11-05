package config

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
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

func TestValidateLabelSize(t *testing.T) {
	for _, dimension := range []string{"width", "height"} {
		for _, tc := range []struct {
			name  string
			value float64
			valid bool
		}{
			{"zero", 0, false},
			{"negative zero", math.Copysign(0, -1), false},
			{"negative", -1, false},
			{"NaN", math.NaN(), false},
			{"positive infinity", math.Inf(1), false},
			{"negative infinity", math.Inf(-1), false},
			{"point overflow", math.MaxFloat64, false},
			{"fraction", 0.25, true},
			{"renderer underflow", math.SmallestNonzeroFloat64, false},
			{"renderer overflow", math.MaxFloat64 / 144, false},
			{"renderer zero boundary", float64(math.SmallestNonzeroFloat32) / 144, false},
			{"beyond largest renderer point", float64(math.MaxFloat32) * 2 / 72, false},
			{"smallest renderer point", float64(math.SmallestNonzeroFloat32) / 72, true},
			{"largest renderer point", float64(math.MaxFloat32) / 72, true},
		} {
			t.Run(dimension+"/"+tc.name, func(t *testing.T) {
				cfg := Defaults()
				if dimension == "width" {
					cfg.LabelW = tc.value
				} else {
					cfg.LabelH = tc.value
				}
				err := cfg.ValidateLabelSize()
				if tc.valid {
					if err != nil {
						t.Fatalf("ValidateLabelSize: %v", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "label "+dimension) {
					t.Fatalf("error = %v, want invalid label %s", err, dimension)
				}
			})
		}
	}
}

// setHome points every platform's home lookup at dir.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("AppData", filepath.Join(dir, "AppData"))
}

func nativeConfigPath(t *testing.T) string {
	t.Helper()
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "fourbysix", "config.yaml")
}

func TestUserConfigPathHonorsXDG(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	xdg := filepath.Join(home, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	// XDG wins even over an existing ~/.config file.
	writeFile(t, filepath.Join(home, ".config", "fourbysix", "config.yaml"), "dpi: 203\n")
	if got, want := UserConfigPath(), filepath.Join(xdg, "fourbysix", "config.yaml"); got != want {
		t.Fatalf("UserConfigPath() = %q, want %q", got, want)
	}
}

func TestUserConfigPathIgnoresRelativeXDG(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "relative/dir")
	if got, want := UserConfigPath(), nativeConfigPath(t); got != want {
		t.Fatalf("UserConfigPath() = %q, want %q", got, want)
	}
}

func TestUserConfigPathFindsExistingDotConfig(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", "")
	want := filepath.Join(home, ".config", "fourbysix", "config.yaml")
	writeFile(t, want, "dpi: 203\n")
	if got := UserConfigPath(); got != want {
		t.Fatalf("UserConfigPath() = %q, want %q", got, want)
	}
}

func TestUserConfigPathDefaultsToNativeDir(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	if got, want := UserConfigPath(), nativeConfigPath(t); got != want {
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
