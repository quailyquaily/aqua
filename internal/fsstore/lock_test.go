package fsstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWithLockRunsCriticalSection(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lockPath, err := BuildLockPath(filepath.Join(root, ".fslocks"), "state.main")
	if err != nil {
		t.Fatalf("BuildLockPath() error = %v", err)
	}

	called := false
	err = WithLock(context.Background(), lockPath, func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock() error = %v", err)
	}
	if !called {
		t.Fatalf("WithLock() did not run critical section")
	}
}
