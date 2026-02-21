package fsstore

import (
	"errors"
	"fmt"
	"os"
)

func ReadText(path string) (string, bool, error) {
	normalizedPath, err := normalizePath(path)
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(normalizedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read text %s: %w", normalizedPath, err)
	}
	return string(data), true, nil
}

func WriteTextAtomic(path string, content string, opts FileOptions) error {
	normalizedPath, err := normalizePath(path)
	if err != nil {
		return err
	}
	return writeAtomic(normalizedPath, []byte(content), opts)
}
