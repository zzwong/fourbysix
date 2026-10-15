package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type saveTemp interface {
	io.Writer
	Close() error
	Chmod(os.FileMode) error
	Name() string
}

type saveOps struct {
	createTemp func(string, string) (saveTemp, error)
	rename     func(string, string) error
	remove     func(string) error
}

func saveFile(path string, write func(io.Writer) error) error {
	return saveFileWithOps(path, write, saveOps{
		createTemp: func(dir, pattern string) (saveTemp, error) { return os.CreateTemp(dir, pattern) },
		rename:     os.Rename,
		remove:     os.Remove,
	})
}

func saveFileWithOps(path string, write func(io.Writer) error, ops saveOps) (err error) {
	// Split does not clean: parent symlinks must be followed before resolving .. .
	parent, leaf := filepath.Split(path)
	if leaf == "" || leaf == "." || leaf == ".." {
		return fmt.Errorf("output must name a regular file: %s", path)
	}
	if parent == "" {
		parent = "."
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	target := filepath.Join(parent, leaf)
	mode := os.FileMode(0o600)
	info, err := os.Lstat(target)
	if err == nil {
		// Reject leaf links (including dangling links), devices and special modes.
		// Replacement deliberately detaches this pathname from any hard links.
		if !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return fmt.Errorf("output must be a regular file without special mode bits: %s", path)
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	temp, err := ops.createTemp(parent, ".fourbysix-*")
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if cleanupErr := ops.remove(temp.Name()); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			err = errors.Join(err, fmt.Errorf("remove temporary output: %w", cleanupErr))
		}
	}()
	// Keep partial bytes private until serialization has finished.
	err = write(temp)
	if err == nil {
		err = temp.Chmod(mode)
	}
	err = errors.Join(err, temp.Close())
	if err != nil {
		return err
	}
	if err = ops.rename(temp.Name(), target); err != nil {
		return err
	}
	committed = true
	return nil
}
