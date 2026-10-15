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

type failingSaveTemp struct {
	*os.File
	stage  string
	closed *int
}

func (f failingSaveTemp) Write(p []byte) (int, error) {
	if f.stage == "write" {
		n, err := f.File.Write(p[:min(7, len(p))])
		if err != nil {
			return n, err
		}
		return n, errSaveInjected
	}
	return f.File.Write(p)
}
func (f failingSaveTemp) Chmod(mode os.FileMode) error {
	if f.stage == "chmod" {
		return errSaveInjected
	}
	return f.File.Chmod(mode)
}
func (f failingSaveTemp) Close() error {
	*f.closed++
	err := f.File.Close()
	if f.stage == "close" {
		return errors.Join(err, errSaveInjected)
	}
	return err
}

func failingSaveOps(stage string, closed *int, renamed *bool) saveOps {
	return saveOps{
		createTemp: func(dir, pattern string) (saveTemp, error) {
			if stage == "create" {
				return nil, errSaveInjected
			}
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			return failingSaveTemp{f, stage, closed}, nil
		},
		rename: func(from, to string) error {
			*renamed = true
			if stage == "rename" {
				return errSaveInjected
			}
			return os.Rename(from, to)
		},
		remove: os.Remove,
	}
}

func assertSavePreserved(t *testing.T, path string, old []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if old == nil {
		if !os.IsNotExist(err) {
			t.Fatalf("absent output changed: %q, %v", got, err)
		}
	} else if err != nil || !bytes.Equal(got, old) {
		t.Fatalf("existing output changed: %q, %v", got, err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".fourbysix-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files remain: %v, %v", matches, err)
	}
}

func TestSaveFailuresPreserveDestination(t *testing.T) {
	for _, stage := range []string{"create", "serialize", "write", "chmod", "close", "rename"} {
		for _, exists := range []bool{false, true} {
			name := stage + "/absent"
			if exists {
				name = stage + "/existing"
			}
			t.Run(name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "out.pdf")
				var old []byte
				if exists {
					old = []byte("previous PDF must survive unchanged")
					if err := os.WriteFile(path, old, 0o640); err != nil {
						t.Fatal(err)
					}
				}
				closed := 0
				renamed := false
				err := saveFileWithOps(path, func(w io.Writer) error {
					_, err := w.Write([]byte("replacement PDF bytes"))
					if stage == "serialize" {
						return errSaveInjected
					}
					return err
				}, failingSaveOps(stage, &closed, &renamed))
				if !errors.Is(err, errSaveInjected) {
					t.Fatalf("error = %v", err)
				}
				wantClosed := 1
				if stage == "create" {
					wantClosed = 0
				}
				if closed != wantClosed {
					t.Fatalf("close calls = %d", closed)
				}
				if renamed != (stage == "rename") {
					t.Fatalf("unexpected rename: %v", renamed)
				}
				assertSavePreserved(t, path, old)
			})
		}
	}
}

func TestRunSaveFailureDoesNotPrint(t *testing.T) {
	for _, stage := range []string{"write", "close", "rename"} {
		for _, exists := range []bool{false, true} {
			t.Run(stage+map[bool]string{true: "/existing", false: "/absent"}[exists], func(t *testing.T) {
				dir := t.TempDir()
				capture := installFakeLP(t)
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				src := writeFixture(t, dir, []testpdf.Page{{}})
				path := filepath.Join(dir, "out.pdf")
				var old []byte
				if exists {
					old = []byte("old PDF")
					if err := os.WriteFile(path, old, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				closed := 0
				renamed := false
				var stdout, stderr bytes.Buffer
				code := runWithSave([]string{src, "-o", path, "--print"}, &stdout, &stderr, func(path string, write func(io.Writer) error) error {
					return saveFileWithOps(path, write, failingSaveOps(stage, &closed, &renamed))
				})
				if code != 1 || !strings.Contains(stderr.String(), errSaveInjected.Error()) || strings.Contains(stdout.String(), "Saved:") {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				assertLPNotCalled(t, capture)
				assertSavePreserved(t, path, old)
			})
		}
	}
}

func TestSaveFilePolicies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix filesystem policies")
	}
	for _, kind := range []string{"symlink", "dangling", "directory", "special mode"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "out.pdf")
			target := filepath.Join(dir, "target")
			old := []byte("target unchanged")
			if kind != "dangling" {
				if err := os.WriteFile(target, old, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			switch kind {
			case "symlink", "dangling":
				err = os.Symlink(target, path)
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "special mode":
				err = os.WriteFile(path, old, 0o600)
				if err == nil {
					err = os.Chmod(path, 0o600|os.ModeSetuid)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			err = saveFile(path, func(w io.Writer) error { called = true; return nil })
			if err == nil || called {
				t.Fatalf("accepted %s: %v", kind, err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("replaced target: %v", err)
			}
			if kind == "dangling" {
				assertSavePreserved(t, target, nil)
			} else {
				assertSavePreserved(t, target, old)
			}
		})
	}
}

func TestRunSaveSuccessAndAliases(t *testing.T) {
	for _, kind := range []string{"new", "overwrite", "same path", "hardlink", "default", "dash", "parent symlink"} {
		t.Run(kind, func(t *testing.T) {
			if (kind == "hardlink" || kind == "parent symlink") && runtime.GOOS == "windows" {
				t.Skip("Unix link behavior")
			}
			dir := t.TempDir()
			t.Chdir(dir)
			src := writeFixture(t, dir, []testpdf.Page{{}, {}})
			input, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			out := "out.pdf"
			wantMode := os.FileMode(0o600)
			switch kind {
			case "overwrite":
				if err := os.WriteFile(out, []byte("old"), 0o640); err != nil {
					t.Fatal(err)
				}
				wantMode = 0o640
			case "same path":
				out = src
				wantMode = 0o644
			case "hardlink":
				if err := os.Link(src, out); err != nil {
					t.Fatal(err)
				}
				wantMode = 0o644
			case "default":
				out = filepath.Join(dir, "return_label.pdf")
			case "dash":
				out = "-output.pdf"
			case "parent symlink":
				if err := os.MkdirAll("target/nested", 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target/nested", "shortcut"); err != nil {
					t.Fatal(err)
				}
				out = "shortcut/../out.pdf"
			}
			args := []string{src}
			if kind != "default" {
				args = append(args, "-o", out)
			}
			code, stdout, stderr := runCLI(t, args...)
			if code != 0 || !strings.Contains(stdout, "Saved: "+out) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			count, sizes := pageSizes(t, out)
			if count != 2 {
				t.Fatalf("count=%d", count)
			}
			for _, size := range sizes {
				if size != [2]float64{288, 432} {
					t.Fatalf("size=%v", size)
				}
			}
			info, err := os.Stat(out)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != wantMode {
				t.Fatalf("mode=%v want=%v", info.Mode(), wantMode)
			}
			if kind != "same path" {
				got, err := os.ReadFile(src)
				if err != nil || !bytes.Equal(got, input) {
					t.Fatal("input changed")
				}
			}
			if kind == "parent symlink" {
				if _, err := os.Stat("out.pdf"); !os.IsNotExist(err) {
					t.Fatal("parent path was cleaned")
				}
			}
			parent, _ := filepath.Split(out)
			matches, err := filepath.Glob(parent + ".fourbysix-*")
			if err != nil || len(matches) != 0 {
				t.Fatalf("leftovers: %v %v", matches, err)
			}
		})
	}
}
