package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitRejectsDanglingConfigLink(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	path := filepath.Join(root, "fourbysix", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "missing-target.yaml")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--init"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("failed init wrote stdout: %q", stdout.String())
	}
	want := "Config already exists at " + path + "\nRemove it first or edit it directly.\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
	if got, err := os.Readlink(path); err != nil || got != target {
		t.Errorf("config link = %q, %v; want unchanged %q", got, err, target)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dangling target was created or inaccessible: %v", err)
	}
}

func TestRunInitSuccessThenRefusal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	path := filepath.Join(root, "fourbysix", "config.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("first init exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "Created "+path+"\n") || stderr.Len() != 0 {
		t.Fatalf("first init stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--init"}, &stdout, &stderr); code != 1 {
		t.Errorf("second init exit = %d, want 1", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "Config already exists at "+path+"\n") {
		t.Errorf("second init stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("second init changed config: %v", err)
	}
}
