package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zzwong/fourbysix/internal/config"
	"github.com/zzwong/fourbysix/internal/testpdf"
)

func writeLabelConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunRejectsInvalidLabelDimensionsWithoutTouchingOutput(t *testing.T) {
	values := []struct {
		name string
		cli  string
		yaml string
	}{
		{"zero", "0", "0"},
		{"negative zero", "-0", "-0.0"},
		{"negative", "-1", "-1"},
		{"NaN", "NaN", ".nan"},
		{"positive infinity", "Inf", ".inf"},
		{"negative infinity", "-Inf", "-.inf"},
		{"quoted NaN", "NaN", "\"NaN\""},
		{"quoted infinity", "+Inf", "\"Inf\""},
		{"quoted negative infinity", "-Inf", "\"-Inf\""},
		{"point overflow", "1e308", "\"1e308\""},
		{"renderer underflow", "1e-50", "\"1e-50\""},
		{"renderer overflow", "1e38", "\"1e38\""},
	}
	for _, axis := range []string{"width", "height"} {
		for _, value := range values {
			for _, source := range []string{"CLI", "user config", "explicit config"} {
				for _, existing := range []bool{false, true} {
					name := fmt.Sprintf("%s/%s/%s/existing=%t", axis, value.name, source, existing)
					t.Run(name, func(t *testing.T) {
						dir := t.TempDir()
						t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
						src := writeFixture(t, dir, []testpdf.Page{{}})
						out := filepath.Join(dir, "return_label.pdf")
						args := []string{src, "-q"}
						if existing {
							out = filepath.Join(dir, "existing.pdf")
							args = append(args, "-o", out)
							if err := os.WriteFile(out, []byte("keep this output"), 0o644); err != nil {
								t.Fatal(err)
							}
						}
						switch source {
						case "CLI":
							label := value.cli + "x6"
							if axis == "height" {
								label = "4x" + value.cli
							}
							args = append(args, "--label", label)
						case "user config":
							writeLabelConfig(t, config.UserConfigPath(), "label:\n  "+axis+": "+value.yaml+"\n")
						case "explicit config":
							path := filepath.Join(dir, "explicit.yaml")
							writeLabelConfig(t, path, "label:\n  "+axis+": "+value.yaml+"\n")
							args = append(args, "--config", path)
						}
						var stdout, stderr bytes.Buffer
						code := run(args, &stdout, &stderr)
						if code != 1 || !strings.Contains(stderr.String(), "Error: label "+axis+" must") || !strings.Contains(stderr.String(), "finite and positive") {
							t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
						}
						if stdout.Len() != 0 {
							t.Fatalf("unexpected stdout: %q", stdout.String())
						}
						data, err := os.ReadFile(out)
						if existing {
							if err != nil || string(data) != "keep this output" {
								t.Fatalf("existing output changed: data = %q, error = %v", data, err)
							}
						} else if !os.IsNotExist(err) {
							t.Fatalf("output should not exist: data = %q, error = %v", data, err)
						}
					})
				}
			}
		}
	}
}

func TestRunValidatesLabelBeforeOpeningInput(t *testing.T) {
	code, _, stderr := runCLI(t, filepath.Join(t.TempDir(), "missing.pdf"), "--label", "NaNx6", "-q")
	if code != 1 || !strings.Contains(stderr, "label width must be finite and positive") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestRunLabelFractionsAndPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		user     string
		explicit string
		label    string
		want     [2]float64
	}{
		{"defaults", "", "", "", [2]float64{288, 432}},
		{"CLI fractions", "", "", "2.25x1.25", [2]float64{162, 90}},
		{"smallest renderer width", "", "", fmt.Sprintf("%gx6", float64(math.SmallestNonzeroFloat32)/72), [2]float64{math.SmallestNonzeroFloat32, 432}},
		{"smallest renderer height", "", "", fmt.Sprintf("4x%g", float64(math.SmallestNonzeroFloat32)/72), [2]float64{288, math.SmallestNonzeroFloat32}},
		{"largest renderer width", "", "", fmt.Sprintf("%gx6", float64(math.MaxFloat32)/72), [2]float64{math.MaxFloat32, 432}},
		{"largest renderer height", "", "", fmt.Sprintf("4x%g", float64(math.MaxFloat32)/72), [2]float64{288, math.MaxFloat32}},
		{"partial user", "label:\n  width: 2.25\n", "", "", [2]float64{162, 432}},
		{"partial explicit", "label:\n  width: 3\n  height: 1.25\n", "label:\n  width: \"2.25\"\n", "", [2]float64{162, 90}},
		{"explicit repairs user", "label:\n  width: .nan\n  height: -.inf\n", "label:\n  width: 2.25\n  height: 1.25\n", "", [2]float64{162, 90}},
		{"CLI repairs configs", "label:\n  width: \"NaN\"\n", "label:\n  height: \"Inf\"\n", "2.25x1.25", [2]float64{162, 90}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
			src := writeFixture(t, dir, []testpdf.Page{{}})
			out := filepath.Join(dir, "out.pdf")
			args := []string{src, "-q", "-o", out}
			if tc.user != "" {
				writeLabelConfig(t, config.UserConfigPath(), tc.user)
			}
			if tc.explicit != "" {
				path := filepath.Join(dir, "explicit.yaml")
				writeLabelConfig(t, path, tc.explicit)
				args = append(args, "--config", path)
			}
			if tc.label != "" {
				args = append(args, "--label", tc.label)
			}
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
			}
			count, sizes := pageSizes(t, out)
			if count != 1 || sizes[0] != tc.want {
				t.Fatalf("pages = %d %v, want 1 at %v", count, sizes, tc.want)
			}
		})
	}
}
