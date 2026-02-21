package fsstore

import (
	"fmt"
	"path/filepath"
	"strings"
)

func normalizePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidPath)
	}
	return filepath.Clean(path), nil
}
