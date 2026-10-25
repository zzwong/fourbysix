package config

import (
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLoadDPIKeepsExactIntegerScalars(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	for _, tc := range []struct {
		name string
		yaml string
		want int
	}{
		{"integer", "203", 203},
		{"quoted integer", "\"203\"", 203},
		{"whole float", "203.0", 203},
		{"quoted whole float", "\"203.0\"", 203},
		{"quoted exponent", "\"2.03e2\"", 203},
		{"maximum integer", strconv.Itoa(maxInt), maxInt},
		{"quoted maximum integer", strconv.Quote(strconv.Itoa(maxInt)), maxInt},
		{"minimum integer", strconv.Itoa(minInt), minInt},
		{"quoted minimum integer", strconv.Quote(strconv.Itoa(minInt)), minInt},
		{"null retains previous", "null", 300},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeFile(t, path, "dpi: "+tc.yaml+"\n")
			cfg, err := Load(path, Defaults())
			if err != nil || cfg.DPI != tc.want {
				t.Fatalf("Load: dpi = %d, error = %v; want %d", cfg.DPI, err, tc.want)
			}
		})
	}
}

func TestLoadDPIRejectsInvalidScalars(t *testing.T) {
	limit := uint64(1) << (strconv.IntSize - 1)
	tooHigh := strconv.FormatUint(limit, 10)
	tooLow := "-" + strconv.FormatUint(limit+1, 10)
	for _, scalar := range []string{
		"203.5", "\"203.5\"", ".nan", ".inf", "-.inf",
		"\"NaN\"", "\"+Inf\"", "\"-Inf\"",
		tooHigh, strconv.Quote(tooHigh), strconv.Quote(tooLow),
		"1e100", "\"1e100\"", "false", "[]", "{}", "\"not a number\"",
	} {
		t.Run(scalar, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeFile(t, path, "dpi: "+scalar+"\n")
			if _, err := Load(path, Defaults()); err == nil || !strings.Contains(err.Error(), "dpi:") {
				t.Fatalf("Load error = %v, want dpi decoding error", err)
			}
		})
	}
}

func TestDPINumberChecksIntegerBoundsBeforeConversion(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name  string
		value any
		valid bool
		want  int
	}{
		{"int", maxInt, true, maxInt},
		{"int64 in range", int64(maxInt), true, maxInt},
		{"uint64 in range", uint64(maxInt), true, maxInt},
		{"uint64 beyond int", uint64(maxInt) + 1, false, 0},
		{"maximum uint64", ^uint64(0), false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := dpiNumber(tc.value)
			if tc.valid {
				if err != nil || !ok || got != tc.want {
					t.Fatalf("dpiNumber = %d, %v, %v; want %d, true, nil", got, ok, err, tc.want)
				}
			} else if err == nil || ok {
				t.Fatalf("dpiNumber = %d, %v, %v; want rejection", got, ok, err)
			}
		})
	}
	if strconv.IntSize == 32 {
		for _, value := range []int64{1 << 31, -(1 << 31) - 1} {
			if _, ok, err := dpiNumber(value); err == nil || ok {
				t.Fatalf("dpiNumber(%d) = ok %v, error %v; want rejection", value, ok, err)
			}
		}
	}
}

func TestDPINumberChecksFloatBoundsBeforeConversion(t *testing.T) {
	limit := math.Ldexp(1, strconv.IntSize-1)
	largestWhole := math.Trunc(math.Nextafter(limit, 0))
	for _, tc := range []struct {
		name  string
		value float64
		valid bool
	}{
		{"largest representable whole float", largestWhole, true},
		{"minimum integer", -limit, true},
		{"upper limit", limit, false},
		{"below lower limit", math.Nextafter(-limit, math.Inf(-1)), false},
		{"fraction", 203.5, false},
		{"NaN", math.NaN(), false},
		{"positive infinity", math.Inf(1), false},
		{"negative infinity", math.Inf(-1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, ok, err := dpiNumber(tc.value)
			if tc.valid {
				if err != nil || !ok || float64(value) != tc.value {
					t.Fatalf("dpiNumber(%v) = %d, %v, %v", tc.value, value, ok, err)
				}
			} else if err == nil || ok {
				t.Fatalf("dpiNumber(%v) = %d, %v, %v; want rejection", tc.value, value, ok, err)
			}
		})
	}
}

func TestValidateDPIAfterOverrides(t *testing.T) {
	for _, value := range []int{-1, 0, 1, 203, int(^uint(0) >> 1)} {
		cfg := Defaults()
		cfg.DPI = value
		if err := cfg.ValidateDPI(); (err == nil) != (value > 0) {
			t.Fatalf("ValidateDPI(%d) = %v", value, err)
		}
	}
	for _, scalar := range []string{"0", "-1"} {
		t.Run(scalar, func(t *testing.T) {
			dir := t.TempDir()
			first := filepath.Join(dir, "first.yaml")
			second := filepath.Join(dir, "second.yaml")
			writeFile(t, first, "dpi: "+scalar+"\nlabel:\n  width: 2\n")
			writeFile(t, second, "dpi: 203\n")
			cfg, err := Load(first, Defaults())
			if err != nil {
				t.Fatalf("Load first: %v", err)
			}
			if cfg.ValidateDPI() == nil {
				t.Fatal("nonpositive effective DPI accepted")
			}
			cfg, err = Load(second, cfg)
			if err != nil || cfg.ValidateDPI() != nil || cfg.DPI != 203 || cfg.LabelW != 2 {
				t.Fatalf("later override = %+v, error = %v", cfg, err)
			}
		})
	}
}
