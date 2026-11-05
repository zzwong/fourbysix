// Package config holds defaults, YAML loading with deep-merge semantics,
// and --init. Load order is defaults -> user config -> --config -> CLI
// flags; each YAML file only overrides the keys it contains, including
// individual keys of the label dict.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"math"
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

// ValidateLabelSize checks the effective dimensions after all overrides.
// PDFium stores page dimensions as float32, so a finite inch value can still
// overflow or become zero after conversion to the renderer's PDF points.
func (c Config) ValidateLabelSize() error {
	for _, dimension := range []struct {
		name  string
		value float64
	}{
		{"width", c.LabelW},
		{"height", c.LabelH},
	} {
		if dimension.value <= 0 || math.IsNaN(dimension.value) || math.IsInf(dimension.value*72, 0) {
			return fmt.Errorf("label %s must be finite and positive in inches and PDF points", dimension.name)
		}
		points := float64(float32(dimension.value * 72))
		if points <= 0 || math.IsInf(points, 0) {
			return fmt.Errorf("label %s must remain finite and positive in PDFium's numeric range", dimension.name)
		}
	}
	return nil
}

// ValidateDPI checks the effective resolution after all overrides.
func (c Config) ValidateDPI() error {
	if c.DPI <= 0 {
		return errors.New("dpi must be positive")
	}
	return nil
}

// UserConfigPath resolves the user config the same way on every platform:
//
//  1. $XDG_CONFIG_HOME/fourbysix/config.yaml when XDG_CONFIG_HOME is an
//     absolute path (the XDG spec says to ignore relative values);
//  2. ~/.config/fourbysix/config.yaml when that file exists, so macOS and
//     Windows users who keep dotfiles in ~/.config are found without
//     setting XDG_CONFIG_HOME;
//  3. os.UserConfigDir()/fourbysix/config.yaml, the platform's native dir
//     (~/Library/Application Support on macOS, %AppData% on Windows).
//
// --init writes to whichever path this returns.
func UserConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return appConfig(xdg)
	}
	home, homeErr := os.UserHomeDir()
	if homeErr == nil {
		dotConfig := appConfig(filepath.Join(home, ".config"))
		if _, err := os.Stat(dotConfig); err == nil {
			return dotConfig
		}
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return appConfig(dir)
	}
	// UserConfigDir is unavailable (no HOME/AppData); fall back to
	// ~/.config so the tool still works.
	return appConfig(filepath.Join(home, ".config"))
}

func appConfig(dir string) string {
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
// A write or close failure may leave a partial new config for inspection.
func Init(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Classify all existing entries consistently, including directories and
	// dangling links. Exclusive creation below still guards the later race.
	if _, err := os.Lstat(path); err == nil {
		return ErrExists
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return ErrExists
	}
	if err != nil {
		return err
	}
	_, err = file.Write(exampleConfig)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
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
			n, ok, err := dpiNumber(val)
			if err != nil {
				return c, fmt.Errorf("dpi: %w", err)
			}
			if ok {
				c.DPI = n
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

// dpiNumber preserves exact integer scalars instead of rounding them through
// float64. Floating-point inputs must be finite whole numbers in int's range;
// the exclusive upper bound avoids float64 rounding MaxInt up to 2^(bits-1).
func dpiNumber(v any) (int, bool, error) {
	invalid := errors.New("must be a finite integer within the platform int range")
	switch n := v.(type) {
	case nil:
		return 0, false, nil
	case int:
		return n, true, nil
	case int64:
		maxInt := int64(^uint(0) >> 1)
		if n < -maxInt-1 || n > maxInt {
			return 0, false, invalid
		}
		return int(n), true, nil
	case uint64:
		if n > uint64(^uint(0)>>1) {
			return 0, false, invalid
		}
		return int(n), true, nil
	case string:
		if value, err := strconv.ParseInt(n, 10, strconv.IntSize); err == nil {
			return int(value), true, nil
		} else if errors.Is(err, strconv.ErrRange) {
			return 0, false, invalid
		}
	}
	n, ok, err := number(v)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, invalid
	}
	limit := math.Ldexp(1, strconv.IntSize-1)
	if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < -limit || n >= limit {
		return 0, false, invalid
	}
	return int(n), true, nil
}
