package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInitPreservesExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	want := []byte("dpi: 203\n# Keep my settings.\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Init(path); err != ErrExists {
		t.Fatalf("Init error = %v, want exact ErrExists", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || !os.SameFile(before, after) || before.Mode() != after.Mode() {
		t.Fatal("Init changed the existing config's bytes, identity, or mode")
	}
}

func TestInitRefusesLeafSymlinks(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "dangling"
		if existing {
			name = "existing target"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			target := filepath.Join(dir, "target.yaml")
			want := []byte("dpi: 203\n")
			if existing {
				if err := os.WriteFile(target, want, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, path); err != nil {
				if runtime.GOOS == "windows" {
					t.Skipf("symlink creation unavailable: %v", err)
				}
				t.Fatal(err)
			}
			if err := Init(path); err != ErrExists {
				t.Errorf("Init error = %v, want exact ErrExists", err)
			}
			if got, err := os.Readlink(path); err != nil || got != target {
				t.Errorf("config link = %q, %v; want unchanged %q", got, err, target)
			}
			if !existing {
				if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("dangling target was created or inaccessible: %v", err)
				}
				return
			}
			got, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("target = %q, %v; want unchanged %q", got, err, want)
			}
		})
	}
}

func TestInitRefusesExistingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Init(path); err != ErrExists {
		t.Fatalf("Init error = %v, want exact ErrExists", err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("existing directory changed: %v", err)
	}
}

func TestInitConcurrentCreationHasOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fourbysix", "config.yaml")
	const callers = 16
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- Init(path)
		}()
	}
	close(start)
	successes := 0
	for range callers {
		err := <-results
		if err == nil {
			successes++
		} else if err != ErrExists {
			t.Errorf("Init error = %v, want nil or exact ErrExists", err)
		}
	}
	if successes != 1 {
		t.Errorf("successful initializers = %d, want 1", successes)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, exampleConfig) {
		t.Fatalf("created config = %q, %v; want embedded example", got, err)
	}
}

func TestInitKeepsCreationPermissions(t *testing.T) {
	dir := t.TempDir()
	control := filepath.Join(dir, "control.yaml")
	if err := os.WriteFile(control, exampleConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(control)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode().Perm() != want.Mode().Perm() {
		t.Fatalf("creation mode = %v, want %v under the same umask", got.Mode(), want.Mode())
	}
}

func TestInitReturnsParentCreationError(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	want := []byte("keep")
	if err := os.WriteFile(parent, want, 0o600); err != nil {
		t.Fatal(err)
	}
	err := Init(filepath.Join(parent, "config.yaml"))
	if err == nil || errors.Is(err, ErrExists) {
		t.Fatalf("Init error = %v, want parent creation error", err)
	}
	got, err := os.ReadFile(parent)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("parent file = %q, %v; want unchanged %q", got, err, want)
	}
}
