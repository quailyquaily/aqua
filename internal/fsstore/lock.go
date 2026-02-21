package fsstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	lockKeyMaxLen = 120
	lockRetryWait = 25 * time.Millisecond
)

func BuildLockPath(lockRoot string, lockKey string) (string, error) {
	lockRoot, err := normalizePath(lockRoot)
	if err != nil {
		return "", err
	}
	lockKey, err = validateLockKey(lockKey)
	if err != nil {
		return "", err
	}
	return filepath.Join(lockRoot, lockKey+".lck"), nil
}

func WithLock(ctx context.Context, lockPath string, fn func() error) error {
	normalizedPath, err := normalizePath(lockPath)
	if err != nil {
		return err
	}
	if fn == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := EnsureDir(filepath.Dir(normalizedPath), defaultDirPerm); err != nil {
		return err
	}
	return withLockFile(ctx, normalizedPath, fn)
}

func validateLockKey(lockKey string) (string, error) {
	lockKey = strings.TrimSpace(lockKey)
	if lockKey == "" {
		return "", fmt.Errorf("%w: empty lock key", ErrInvalidPath)
	}
	if len(lockKey) > lockKeyMaxLen {
		return "", fmt.Errorf("%w: lock key too long", ErrInvalidPath)
	}
	if strings.ToLower(lockKey) != lockKey {
		return "", fmt.Errorf("%w: lock key must be lowercase", ErrInvalidPath)
	}
	if strings.HasPrefix(lockKey, ".") || strings.HasSuffix(lockKey, ".") {
		return "", fmt.Errorf("%w: lock key cannot start or end with dot", ErrInvalidPath)
	}
	for _, r := range lockKey {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return "", fmt.Errorf("%w: invalid lock key character %q", ErrInvalidPath, r)
	}
	return lockKey, nil
}

func writeLockDebugMetadata(file *os.File, lockPath string) {
	if file == nil {
		return
	}
	host, _ := os.Hostname()
	payload := map[string]any{
		"lock_path":     lockPath,
		"pid":           os.Getpid(),
		"hostname":      host,
		"acquired_at":   time.Now().UTC().Format(time.RFC3339Nano),
		"lock_owner_id": fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UTC().UnixNano()),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_ = file.Truncate(0)
	_, _ = file.Seek(0, 0)
	_, _ = file.Write(data)
	_ = file.Sync()
}

func waitForLockRetry(ctx context.Context, lockPath string) error {
	timer := time.NewTimer(lockRetryWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: %s: %v", ErrLockTimeout, lockPath, ctx.Err())
	case <-timer.C:
		return nil
	}
}
