package fsstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const defaultIndexVersion = 1

type IndexEntry struct {
	Ref       string          `json:"ref,omitempty"`
	Rev       uint64          `json:"rev,omitempty"`
	Hash      string          `json:"hash,omitempty"`
	UpdatedAt time.Time       `json:"updated_at,omitempty"`
	Meta      json.RawMessage `json:"meta,omitempty"`
}

type IndexFile struct {
	Version int                   `json:"version"`
	Entries map[string]IndexEntry `json:"entries"`
}

func ReadIndex(path string) (IndexFile, bool, error) {
	var out IndexFile
	ok, err := ReadJSON(path, &out)
	if err != nil {
		return IndexFile{}, false, err
	}
	if !ok {
		return IndexFile{}, false, nil
	}
	if out.Version == 0 {
		out.Version = defaultIndexVersion
	}
	if out.Entries == nil {
		out.Entries = map[string]IndexEntry{}
	}
	return out, true, nil
}

func WriteIndexAtomic(path string, index IndexFile, opts FileOptions) error {
	if index.Version == 0 {
		index.Version = defaultIndexVersion
	}
	if index.Entries == nil {
		index.Entries = map[string]IndexEntry{}
	}
	return WriteJSONAtomic(path, index, opts)
}

func MutateIndex(ctx context.Context, path string, lockPath string, opts FileOptions, fn func(*IndexFile) error) error {
	if fn == nil {
		return fmt.Errorf("mutate index: nil mutator")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return WithLock(ctx, lockPath, func() error {
		index, ok, err := ReadIndex(path)
		if err != nil {
			return err
		}
		if !ok {
			index = IndexFile{
				Version: defaultIndexVersion,
				Entries: map[string]IndexEntry{},
			}
		}
		if err := fn(&index); err != nil {
			return err
		}
		return WriteIndexAtomic(path, index, opts)
	})
}
