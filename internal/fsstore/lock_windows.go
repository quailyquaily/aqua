//go:build windows

package fsstore

import (
	"context"
	"errors"
	"fmt"
	"os"
)

func withLockFile(ctx context.Context, lockPath string, fn func() error) error {
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, defaultFilePerm)
		if err == nil {
			defer func() {
				_ = file.Close()
				_ = os.Remove(lockPath)
			}()
			writeLockDebugMetadata(file, lockPath)
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: open %s: %v", ErrLockUnavailable, lockPath, err)
		}
		if waitErr := waitForLockRetry(ctx, lockPath); waitErr != nil {
			return waitErr
		}
	}
}
