//go:build !windows

package fsstore

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func withLockFile(ctx context.Context, lockPath string, fn func() error) error {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, defaultFilePerm)
	if err != nil {
		return fmt.Errorf("%w: open %s: %v", ErrLockUnavailable, lockPath, err)
	}
	defer file.Close()

	fd := int(file.Fd())
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			if waitErr := waitForLockRetry(ctx, lockPath); waitErr != nil {
				return waitErr
			}
			continue
		}
		return fmt.Errorf("%w: flock %s: %v", ErrLockUnavailable, lockPath, err)
	}
	defer func() {
		_ = unix.Flock(fd, unix.LOCK_UN)
	}()

	writeLockDebugMetadata(file, lockPath)
	return fn()
}
