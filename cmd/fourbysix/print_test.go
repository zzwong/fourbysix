package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/zzwong/fourbysix/internal/testpdf"
)

// installFakeLP replaces PATH entirely so these tests cannot reach a real
// printer, even if the fake command is missing or fails to start.
func installFakeLP(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake lp requires a POSIX shell")
	}
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("FOURBYSIX_LP_CAPTURE", dir)
	t.Setenv("FOURBYSIX_LP_EXIT", "0")
	script := `#!/bin/sh
printf '%s\000' "$@" > "$FOURBYSIX_LP_CAPTURE/args"
for operand do :; done
case "$operand" in
    -*) printf 'unexpected option or stdin operand\n' >&2; exit 98 ;;
esac
/bin/cat "$operand" > "$FOURBYSIX_LP_CAPTURE/pdf" || exit 99
printf 'fake lp stdout\n'
printf 'fake lp stderr\n' >&2
exit "$FOURBYSIX_LP_EXIT"
`
	if err := os.WriteFile(filepath.Join(dir, "lp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertLPNotCalled(t *testing.T, capture string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(capture, "args")); !os.IsNotExist(err) {
		t.Fatalf("lp should not have been called: %v", err)
	}
}

func TestRunPrintOutputPaths(t *testing.T) {
	for _, tc := range []struct {
		name    string
		output  string
		operand string
		printer string
	}{
		{"relative", "label.pdf", "label.pdf", ""},
		{"absolute", "", "", ""},
		{"spaces", "label with spaces.pdf", "label with spaces.pdf", "Label Printer"},
		{"unicode", "étiquette-東京.pdf", "étiquette-東京.pdf", "ラベル"},
		{"leading dash", "-label.pdf", "./-label.pdf", ""},
		{"help option", "--help", "./--help", ""},
		{"stdin marker", "-", "./-", ""},
		{"explicit relative", "./-label.pdf", "./-label.pdf", ""},
		{"nested dash", "labels/-label.pdf", "labels/-label.pdf", ""},
		{"lexical components", "-labels//./label.pdf", "./-labels//./label.pdf", ""},
		{"parent component", "-labels/../label.pdf", "./-labels/../label.pdf", ""},
		{"symlink parent", "-shortcut/../label.pdf", "./-shortcut/../label.pdf", ""},
		{"printer argument", "-label.pdf", "./-label.pdf", "-d printer; $(ignored)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			for _, path := range []string{"labels", "-labels", "target/nested"} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			capture := installFakeLP(t)
			if tc.name == "symlink parent" {
				if err := os.Symlink("target/nested", "-shortcut"); err != nil {
					t.Fatal(err)
				}
			}
			output, operand := tc.output, tc.operand
			if tc.name == "absolute" {
				output = filepath.Join(dir, "label.pdf")
				operand = output
			}
			src := writeFixture(t, dir, []testpdf.Page{{}})
			args := []string{src, "-o", output, "--print"}
			wantArgs := []string{operand}
			if tc.printer != "" {
				args = append(args, "--printer", tc.printer)
				wantArgs = []string{"-d", tc.printer, operand}
			}
			code, stdout, stderr := runCLI(t, args...)
			if code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if !strings.Contains(stdout, "\nSaved: "+output+"\n") {
				t.Fatalf("stdout missing original output path: %q", stdout)
			}
			if !strings.HasSuffix(stdout, "fake lp stdout\n") || !strings.HasSuffix(stderr, "fake lp stderr\n") {
				t.Fatalf("lp streams not forwarded: stdout = %q, stderr = %q", stdout, stderr)
			}
			raw, err := os.ReadFile(filepath.Join(capture, "args"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasSuffix(raw, []byte{0}) {
				t.Fatalf("lp arguments missing terminator: %q", raw)
			}
			gotArgs := strings.Split(string(raw[:len(raw)-1]), "\x00")
			if !slices.Equal(gotArgs, wantArgs) {
				t.Fatalf("lp args = %q, want %q", gotArgs, wantArgs)
			}
			saved, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			printed, err := os.ReadFile(filepath.Join(capture, "pdf"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(saved, []byte("%PDF-")) || !bytes.Equal(printed, saved) {
				t.Fatal("lp did not read the saved PDF")
			}
			if tc.name == "symlink parent" {
				if _, err := os.Stat("label.pdf"); !os.IsNotExist(err) {
					t.Fatalf("output path was cleaned: %v", err)
				}
			}
		})
	}
}

func TestRunPrintDefaultOutputPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	capture := installFakeLP(t)
	src := writeFixture(t, dir, []testpdf.Page{{}})
	if err := os.Rename(src, "-input.pdf"); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI(t, "--print", "-q", "--", "-input.pdf")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	args, err := os.ReadFile(filepath.Join(capture, "args"))
	if err != nil || string(args) != "./-input_label.pdf\x00" {
		t.Fatalf("lp args = %q, error = %v", args, err)
	}
}

func TestRunPrintFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
	}{
		{"exit status", 7},
		{"missing lp", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			capture := installFakeLP(t)
			t.Setenv("FOURBYSIX_LP_EXIT", "7")
			if tc.name == "missing lp" {
				if err := os.Remove(filepath.Join(capture, "lp")); err != nil {
					t.Fatal(err)
				}
			}
			src := writeFixture(t, dir, []testpdf.Page{{}})
			out := filepath.Join(dir, "label.pdf")
			code, stdout, stderr := runCLI(t, src, "-o", out, "--print", "-q")
			if code != tc.code {
				t.Fatalf("exit = %d, want %d; stderr = %q", code, tc.code, stderr)
			}
			if tc.name == "missing lp" {
				if !strings.Contains(stderr, "Error: lp:") || stdout != "" {
					t.Fatalf("stdout = %q, stderr = %q", stdout, stderr)
				}
				assertLPNotCalled(t, capture)
			} else if stdout != "fake lp stdout\n" || stderr != "fake lp stderr\n" {
				t.Fatalf("stdout = %q, stderr = %q", stdout, stderr)
			}
			if count, _ := pageSizes(t, out); count != 1 {
				t.Fatalf("saved output has %d pages, want 1", count)
			}
		})
	}
}

func TestRunDoesNotPrintWithoutFlag(t *testing.T) {
	dir := t.TempDir()
	capture := installFakeLP(t)
	src := writeFixture(t, dir, []testpdf.Page{{}})
	out := filepath.Join(dir, "label.pdf")
	code, stdout, stderr := runCLI(t, src, "-o", out, "--printer", "Label Printer", "-q")
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	assertLPNotCalled(t, capture)
}

func TestRunDoesNotPrintAfterOutputFailure(t *testing.T) {
	for _, failure := range []string{"create", "write"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			capture := installFakeLP(t)
			src := writeFixture(t, dir, []testpdf.Page{{}})
			out := dir // Creating a file over a directory must fail.
			if failure == "write" {
				if runtime.GOOS != "linux" {
					t.Skip("write failure requires /dev/full")
				}
				if _, err := os.Stat("/dev/full"); err != nil {
					t.Skipf("/dev/full unavailable: %v", err)
				}
				out = filepath.Join(dir, "label.pdf")
				if err := os.Symlink("/dev/full", out); err != nil {
					t.Fatal(err)
				}
			}
			code, stdout, stderr := runCLI(t, src, "-o", out, "--print", "-q")
			if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "Error:") {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
			}
			assertLPNotCalled(t, capture)
		})
	}
}
