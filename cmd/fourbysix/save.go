package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type saveFile interface {
	io.Writer
	Close() error
	Chmod(os.FileMode) error
	Name() string
}

type saveOps struct {
	createTemp func(string, string) (saveFile, error)
	rename     func(string, string) error
	remove     func(string) error
}

func defaultSaveOps() saveOps {
	return saveOps{
		createTemp: func(dir, pattern string) (saveFile, error) {
			return os.CreateTemp(dir, pattern)
		},
		rename: os.Rename,
		remove: os.Remove,
	}
}

func saveOutput(path string, write func(io.Writer) error) error {
	return saveOutputWithOps(path, write, defaultSaveOps())
}

// Replacement protects old bytes through reported pre-commit failures. It
// does not promise crash durability or safety against concurrent path changes.
func saveOutputWithOps(path string, write func(io.Writer) error, ops saveOps) (err error) {
	// Split preserves the parent's symlink/.. semantics; Dir would clean it
	// before filesystem traversal and could select a different directory.
	parent, leaf := filepath.Split(path)
	if leaf == "" || leaf == "." || leaf == ".." {
		return fmt.Errorf("output must name a regular file: %q", path)
	}
	if parent == "" {
		parent = "."
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	target := filepath.Join(parent, leaf)
	info, statErr := os.Lstat(target)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if statErr == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("output is not a regular file: %q", path)
	}
	f, err := ops.createTemp(parent, ".fourbysix-*.tmp")
	if err != nil {
		return err
	}
	closed, committed := false, false
	defer func() {
		if !closed {
			err = errors.Join(err, f.Close())
		}
		if !committed {
			if cleanupErr := ops.remove(f.Name()); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				err = errors.Join(err, fmt.Errorf("remove temporary output: %w", cleanupErr))
			}
		}
	}()
	if err = write(f); err != nil {
		return err
	}
	if statErr == nil {
		// Restore ordinary permission bits, never special mode bits. New
		// destinations retain CreateTemp's restrictive mode (0600 before umask).
		if err = f.Chmod(info.Mode().Perm()); err != nil {
			return err
		}
	}
	err = f.Close()
	closed = true
	if err != nil {
		return err
	}
	if err = ops.rename(f.Name(), target); err != nil {
		return err
	}
	committed = true
	return nil
}
