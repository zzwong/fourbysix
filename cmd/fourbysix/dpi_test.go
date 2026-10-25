package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/zzwong/fourbysix/internal/config"
)

// Isolate all platform-specific user-config roots, not the real user's files.
func isolateDPIConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	return config.UserConfigPath()
}

func writeDPIConfig(t *testing.T, path, scalar string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("dpi: "+scalar+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunRejectsInvalidConfigDPIBeforeInput(t *testing.T) {
	for _, source := range []string{"user", "explicit"} {
		for _, scalar := range []string{"0", "-1", "203.5", ".nan", ".inf", "\"-Inf\"", "1e100", "false", "[]"} {
			for _, existing := range []bool{false, true} {
				name := source + "/" + scalar + "/absent-output"
				if existing {
					name = source + "/" + scalar + "/existing-output"
				}
				t.Run(name, func(t *testing.T) {
					userPath := isolateDPIConfig(t)
					dir := t.TempDir()
					missing := filepath.Join(dir, "missing.pdf")
					output := filepath.Join(dir, "output.pdf")
					sentinel := []byte("existing output must remain unchanged")
					if existing {
						if err := os.WriteFile(output, sentinel, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					args := []string{missing, "--quiet", "--output", output}
					cfgPath := userPath
					if source == "explicit" {
						cfgPath = filepath.Join(dir, "explicit.yaml")
						args = append(args, "--config", cfgPath)
					}
					writeDPIConfig(t, cfgPath, scalar)
					saveCalled := false
					var stdout, stderr bytes.Buffer
					code := runWithSave(args, &stdout, &stderr, func(string, func(io.Writer) error) error {
						saveCalled = true
						return errors.New("unexpected save")
					})
					if code != 1 || !strings.Contains(stderr.String(), "Error: dpi") || strings.Contains(stderr.String(), "not found") {
						t.Fatalf("exit = %d, stderr = %q; want DPI error before input lookup", code, stderr.String())
					}
					if saveCalled || stdout.Len() != 0 {
						t.Fatalf("save called = %v, stdout = %q", saveCalled, stdout.String())
					}
					got, err := os.ReadFile(output)
					if existing {
						if err != nil || !bytes.Equal(got, sentinel) {
							t.Fatalf("output changed: %q, error %v", got, err)
						}
					} else if !os.IsNotExist(err) {
						t.Fatalf("absent output was created or lookup failed: %v", err)
					}
				})
			}
		}
	}
}

func TestRunDPIOverridePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		user     string
		explicit string
		flags    []string
		wantDPI  bool
	}{
		{"CLI repairs zero", "0", "", []string{"--dpi", "203"}, false},
		{"CLI repairs negative", "-1", "", []string{"--dpi", "203"}, false},
		{"explicit repairs user", "0", "203", nil, false},
		{"CLI repairs explicit", "203", "-1", []string{"--dpi", "300"}, false},
		{"null retains earlier", "203", "null", nil, false},
		{"null does not repair zero", "0", "null", nil, true},
		{"CLI does not repair malformed user", "203.5", "", []string{"--dpi", "203"}, true},
		{"explicit does not repair malformed user", ".inf", "203", nil, true},
		{"CLI does not repair malformed explicit", "203", "false", []string{"--dpi", "203"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userPath := isolateDPIConfig(t)
			dir := t.TempDir()
			writeDPIConfig(t, userPath, tc.user)
			args := []string{filepath.Join(dir, "missing.pdf"), "--quiet"}
			if tc.explicit != "" {
				path := filepath.Join(dir, "explicit.yaml")
				writeDPIConfig(t, path, tc.explicit)
				args = append(args, "--config", path)
			}
			args = append(args, tc.flags...)
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("exit = %d, stderr = %q; expected the deliberate missing-input or DPI error", code, stderr.String())
			}
			if tc.wantDPI {
				if !strings.Contains(stderr.String(), "Error: dpi") || strings.Contains(stderr.String(), "not found") {
					t.Fatalf("stderr = %q, want DPI error before input lookup", stderr.String())
				}
			} else if !strings.Contains(stderr.String(), "not found") || strings.Contains(stderr.String(), "Error: dpi") {
				t.Fatalf("stderr = %q, want effective DPI accepted before missing-input error", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}
}

func TestRunKeepsCLIPositiveDPIError(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			isolateDPIConfig(t)
			var stdout, stderr bytes.Buffer
			code := run([]string{filepath.Join(t.TempDir(), "missing.pdf"), "--quiet", "--dpi", value}, &stdout, &stderr)
			if code != 1 || !strings.Contains(stderr.String(), "Error: --dpi must be positive") {
				t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
			}
		})
	}
}

func TestRunKeepsCLIIntegerDPIParseError(t *testing.T) {
	tooHigh := strconv.FormatUint(uint64(1)<<(strconv.IntSize-1), 10)
	for _, value := range []string{"203.5", "NaN", tooHigh} {
		t.Run(value, func(t *testing.T) {
			isolateDPIConfig(t)
			var stdout, stderr bytes.Buffer
			code := run([]string{filepath.Join(t.TempDir(), "missing.pdf"), "--quiet", "--dpi", value}, &stdout, &stderr)
			if code != 1 || stderr.String() != usageText || stdout.Len() != 0 {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q; want original flag-parse usage error", code, stdout.String(), stderr.String())
			}
		})
	}
}
