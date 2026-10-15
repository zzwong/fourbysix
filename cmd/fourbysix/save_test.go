package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zzwong/fourbysix/internal/testpdf"
)

var errSaveInjected = errors.New("injected save failure")

type failingSaveFile struct {
	*os.File
	failure string
}

func (f *failingSaveFile) Write(p []byte) (int, error) {
	if f.failure == "write" {
		n := min(7, len(p))
		n, err := f.File.Write(p[:n])
		if err != nil {
			return n, err
		}
		return n, errSaveInjected
	}
	return f.File.Write(p)
}

func (f *failingSaveFile) Close() error {
	err := f.File.Close()
	if f.failure == "close" {
		return errors.Join(err, errSaveInjected)
	}
	return err
}

func (f *failingSaveFile) Chmod(mode os.FileMode) error {
	if f.failure == "chmod" {
		return errSaveInjected
	}
	return f.File.Chmod(mode)
}

func injectedSaveOps(failure string) saveOps {
	ops := defaultSaveOps()
	ops.createTemp = func(dir, pattern string) (saveFile, error) {
		if failure == "create" {
			return nil, errSaveInjected
		}
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		return &failingSaveFile{File: f, failure: failure}, nil
	}
	if failure == "rename" {
		ops.rename = func(string, string) error { return errSaveInjected }
	}
	return ops
}

func assertNoSaveTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fourbysix-") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
}

func TestSaveOutputFailuresPreserveDestination(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, failure := range []string{"create", "serialize", "write", "chmod", "close", "rename"} {
			if failure == "chmod" && !existing {
				continue // New files keep CreateTemp permissions without chmod.
			}
			name := "absent/" + failure
			if existing {
				name = "existing/" + failure
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "label.pdf")
				old := []byte("previous complete output")
				if existing {
					if err := os.WriteFile(path, old, 0o640); err != nil {
						t.Fatal(err)
					}
				}
				err := saveOutputWithOps(path, func(w io.Writer) error {
					if failure == "serialize" {
						return errSaveInjected
					}
					_, err := w.Write([]byte("replacement PDF bytes"))
					return err
				}, injectedSaveOps(failure))
				if !errors.Is(err, errSaveInjected) {
					t.Fatalf("error = %v, want injected failure", err)
				}
				got, err := os.ReadFile(path)
				if existing {
					if err != nil || !bytes.Equal(got, old) {
						t.Fatalf("existing output changed: %q, %v", got, err)
					}
				} else if !os.IsNotExist(err) {
					t.Fatalf("absent output created: %q, %v", got, err)
				}
				assertNoSaveTemps(t, dir)
			})
		}
	}
}

func TestSaveOutputSuccessAndModes(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "overwrite"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "label.pdf")
			if existing {
				if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o640); err != nil {
					t.Fatal(err)
				}
			}
			want := []byte("complete new output")
			if err := saveOutput(path, func(w io.Writer) error {
				_, err := w.Write(want)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("output = %q, %v", got, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" {
				if existing && info.Mode().Perm() != 0o640 {
					t.Fatalf("mode = %o, want 640", info.Mode().Perm())
				}
				if !existing && info.Mode().Perm() & ^os.FileMode(0o600) != 0 {
					t.Fatalf("new mode too permissive: %o", info.Mode().Perm())
				}
			}
			assertNoSaveTemps(t, dir)
		})
	}
}

func TestSaveOutputRejectsLeafLinksAndDirectory(t *testing.T) {
	for _, kind := range []string{"symlink", "dangling", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "label.pdf")
			target := filepath.Join(dir, "target")
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			default:
				if kind == "symlink" {
					if err := os.WriteFile(target, []byte("old target"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			called := false
			err := saveOutput(path, func(io.Writer) error { called = true; return nil })
			if err == nil || called {
				t.Fatalf("error = %v, writer called = %v", err, called)
			}
			info, err := os.Lstat(path)
			if err != nil || (kind == "directory" && !info.IsDir()) ||
				(kind != "directory" && info.Mode()&os.ModeSymlink == 0) {
				t.Fatalf("destination changed: %v, %v", info, err)
			}
			if kind == "symlink" {
				got, err := os.ReadFile(target)
				if err != nil || string(got) != "old target" {
					t.Fatalf("symlink target changed: %q, %v", got, err)
				}
			}
			if kind == "dangling" {
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Fatalf("dangling target created: %v", err)
				}
			}
			assertNoSaveTemps(t, dir)
		})
	}
}

func TestRunSaveFailuresDoNotReportOrPrintSuccess(t *testing.T) {
	for _, failure := range []string{"write", "close", "rename"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			capture := installFakeLP(t)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			src := writeFixture(t, dir, []testpdf.Page{{}})
			path := filepath.Join(dir, "label.pdf")
			old := []byte("previous complete output")
			if err := os.WriteFile(path, old, 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := runWithSave([]string{src, "-o", path, "--print"}, &stdout, &stderr,
				func(path string, write func(io.Writer) error) error {
					return saveOutputWithOps(path, write, injectedSaveOps(failure))
				})
			if code != 1 || !strings.Contains(stderr.String(), "Error:") ||
				strings.Contains(stdout.String(), "Saved:") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			assertLPNotCalled(t, capture)
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, old) {
				t.Fatalf("output changed: %q, %v", got, err)
			}
			assertNoSaveTemps(t, dir)
		})
	}
}

func TestRunSaveInputAliases(t *testing.T) {
	for _, kind := range []string{"same path", "hard link"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			src := writeFixture(t, dir, []testpdf.Page{{}, {}})
			old, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			out := src
			if kind == "hard link" {
				out = filepath.Join(dir, "label.pdf")
				if err := os.Link(src, out); err != nil {
					t.Skipf("hard links unavailable: %v", err)
				}
			}
			code, _, stderr := runCLI(t, src, "-o", out, "-q")
			if code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			count, sizes := pageSizes(t, out)
			if count != 2 || len(sizes) != 2 {
				t.Fatalf("page count = %d, sizes = %v", count, sizes)
			}
			for _, size := range sizes {
				if size != [2]float64{288, 432} {
					t.Fatalf("page size = %v", size)
				}
			}
			if kind == "hard link" {
				got, err := os.ReadFile(src)
				if err != nil || !bytes.Equal(got, old) {
					t.Fatalf("input hard link changed: %v", err)
				}
			}
			assertNoSaveTemps(t, dir)
		})
	}
}

func TestSaveOutputPreservesParentTraversal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("target/nested", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target/nested", "-shortcut"); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ops := defaultSaveOps()
	create := ops.createTemp
	ops.createTemp = func(parent, pattern string) (saveFile, error) {
		got, err := os.Stat(parent)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.Stat("target")
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(got, want) {
			t.Fatalf("temporary file parent = %q, want target", parent)
		}
		return create(parent, pattern)
	}
	if err := saveOutputWithOps("-shortcut/../label.pdf", func(w io.Writer) error {
		_, err := io.WriteString(w, "new output")
		return err
	}, ops); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("target/label.pdf")
	if err != nil || string(got) != "new output" {
		t.Fatalf("output = %q, %v", got, err)
	}
	if _, err := os.Lstat("label.pdf"); !os.IsNotExist(err) {
		t.Fatalf("lexically cleaned destination appeared: %v", err)
	}
	assertNoSaveTemps(t, "target")
}

func TestSaveOutputRejectsDevice(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("device check uses Linux /dev/null")
	}
	before, err := os.Lstat("/dev/null")
	if err != nil {
		t.Skipf("/dev/null unavailable: %v", err)
	}
	if before.Mode()&os.ModeDevice == 0 {
		t.Skip("/dev/null is not a device")
	}
	called := false
	ops := defaultSaveOps()
	ops.createTemp = func(string, string) (saveFile, error) {
		called = true
		return nil, errSaveInjected
	}
	err = saveOutputWithOps("/dev/null", func(io.Writer) error {
		t.Fatal("writer called for device")
		return nil
	}, ops)
	if err == nil || called {
		t.Fatalf("error = %v, create called = %v", err, called)
	}
	after, err := os.Lstat("/dev/null")
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("device changed: %v", err)
	}
}
