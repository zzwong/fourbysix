// Package config holds defaults, YAML loading with deep-merge semantics,
// and --init. Load order is defaults -> user config -> --config -> CLI
// flags; each YAML file only overrides the keys it contains, including
// individual keys of the label dict.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"go.yaml.in/yaml/v3"
)

//go:embed config.example.yaml
var exampleConfig []byte

// Config is the resolved tool configuration.
type Config struct {
	LabelW       float64 // inches
	LabelH       float64 // inches
	DPI          int
	Margin       float64 // points
	Rotation     string
	Threshold    bool
	OutputSuffix string
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		LabelW:       4,
		LabelH:       6,
		DPI:          300,
		Margin:       8,
		Rotation:     "auto",
		Threshold:    false,
		OutputSuffix: "_label",
	}
}

// UserConfigPath resolves the user config in the platform config dir:
// $XDG_CONFIG_HOME/fourbysix/config.yaml or
// os.UserConfigDir()/fourbysix/config.yaml.
func UserConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		// Fall back to $HOME/.config so the tool still works where
		// UserConfigDir is unavailable.
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "fourbysix", "config.yaml")
}

// Load merges a YAML config file onto cfg. Unknown keys are ignored, and a
// partial "label" dict only overrides the keys present.
func Load(path string, cfg Config) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if m == nil {
		return cfg, nil
	}
	return cfg.apply(m)
}

// ErrExists is returned by Init when the target file is already present.
var ErrExists = errors.New("config already exists")

// Init writes the embedded example config to path after creating its parent
// directory, refusing to overwrite an existing file.
func Init(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return ErrExists
	}
	return os.WriteFile(path, exampleConfig, 0o644)
}

func (c Config) apply(m map[string]any) (Config, error) {
	for key, val := range m {
		switch key {
		case "label":
			sub, ok := toStringMap(val)
			if !ok {
				continue
			}
			if w, ok, err := number(sub["width"]); err != nil {
				return c, err
			} else if ok {
				c.LabelW = w
			}
			if h, ok, err := number(sub["height"]); err != nil {
				return c, err
			} else if ok {
				c.LabelH = h
			}
		case "dpi":
			n, ok, err := number(val)
			if err != nil {
				return c, fmt.Errorf("dpi: %w", err)
			}
			if ok {
				c.DPI = int(n)
			}
		case "margin":
			n, ok, err := number(val)
			if err != nil {
				return c, fmt.Errorf("margin: %w", err)
			}
			if ok {
				c.Margin = n
			}
		case "rotation":
			if s, ok := val.(string); ok {
				c.Rotation = s
			}
		case "threshold":
			if b, ok := val.(bool); ok {
				c.Threshold = b
			}
		case "output_suffix":
			if s, ok := val.(string); ok {
				c.OutputSuffix = s
			}
		}
	}
	return c, nil
}

func toStringMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	return nil, false
}

// number accepts the scalars yaml.v3 produces for numeric values — int,
// float — plus quoted numbers, so hand-edited configs like `dpi: "203"`
// still load.
func number(v any) (float64, bool, error) {
	switch n := v.(type) {
	case int:
		return float64(n), true, nil
	case int64:
		return float64(n), true, nil
	case uint64:
		return float64(n), true, nil
	case float64:
		return n, true, nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false, fmt.Errorf("%q is not a number", n)
		}
		return f, true, nil
	default:
		return 0, false, nil
	}
}
