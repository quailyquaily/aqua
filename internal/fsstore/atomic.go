package fsstore

import (
	"fmt"
	"os"
	"path/filepath"
)

func EnsureDir(path string, perm os.FileMode) error {
	normalized, err := normalizePath(path)
	if err != nil {
		return err
	}
	if perm == 0 {
		perm = defaultDirPerm
	}
	if err := os.MkdirAll(normalized, perm); err != nil {
		return fmt.Errorf("fsstore ensure dir %s: %w", normalized, err)
	}
	return nil
}

func writeAtomic(path string, content []byte, opts FileOptions) error {
	normalizedPath, err := normalizePath(path)
	if err != nil {
		return err
	}
	opts = normalizeFileOptions(opts)

	parentDir := filepath.Dir(normalizedPath)
	if err := EnsureDir(parentDir, opts.DirPerm); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(parentDir, filepath.Base(normalizedPath)+".tmp.*")
	if err != nil {
		return fmt.Errorf("%w: create temp for %s: %v", ErrAtomicWriteFailed, normalizedPath, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	defer cleanup()

	if _, err := tmp.Write(content); err != nil {
		return fmt.Errorf("%w: write temp for %s: %v", ErrAtomicWriteFailed, normalizedPath, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("%w: sync temp for %s: %v", ErrAtomicWriteFailed, normalizedPath, err)
	}
	if err := tmp.Chmod(opts.FilePerm); err != nil {
		return fmt.Errorf("%w: chmod temp for %s: %v", ErrAtomicWriteFailed, normalizedPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: close temp for %s: %v", ErrAtomicWriteFailed, normalizedPath, err)
	}
	if err := os.Rename(tmpPath, normalizedPath); err != nil {
		return fmt.Errorf("%w: rename temp for %s: %v", ErrAtomicWriteFailed, normalizedPath, err)
	}

	// Best effort directory sync for durability; ignore failures.
	if dirFD, err := os.Open(parentDir); err == nil {
		_ = dirFD.Sync()
		_ = dirFD.Close()
	}
	return nil
}
