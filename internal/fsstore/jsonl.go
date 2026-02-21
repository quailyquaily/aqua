package fsstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type JSONLWriter struct {
	path string
	opts JSONLOptions

	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	size   int64
	closed bool

	now func() time.Time
}

func NewJSONLWriter(path string, opts JSONLOptions) (*JSONLWriter, error) {
	normalizedPath, err := normalizePath(path)
	if err != nil {
		return nil, err
	}
	opts = normalizeJSONLOptions(opts)

	w := &JSONLWriter{
		path: normalizedPath,
		opts: opts,
		now:  time.Now,
	}
	if err := w.openLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *JSONLWriter) AppendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("%w: jsonl encode %s: %v", ErrEncodeFailed, w.path, err)
	}
	data = append(data, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	return w.appendBytesLocked(data)
}

func (w *JSONLWriter) AppendLine(line string) error {
	if strings.ContainsRune(line, '\n') {
		return fmt.Errorf("%w: line contains newline", ErrInvalidPath)
	}
	line = strings.TrimSuffix(line, "\r")
	data := append([]byte(line), '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	return w.appendBytesLocked(data)
}

func (w *JSONLWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.writer != nil {
		_ = w.writer.Flush()
	}
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		w.writer = nil
		w.size = 0
		return err
	}
	return nil
}

func (w *JSONLWriter) appendBytesLocked(data []byte) error {
	if w.closed {
		return fmt.Errorf("jsonl writer closed")
	}
	if err := w.rotateIfNeededLocked(int64(len(data))); err != nil {
		return err
	}
	n, err := w.writer.Write(data)
	if err != nil {
		return err
	}
	w.size += int64(n)
	if w.opts.FlushEachWrite || w.opts.SyncEachWrite {
		if err := w.writer.Flush(); err != nil {
			return err
		}
	}
	if w.opts.SyncEachWrite {
		if err := w.file.Sync(); err != nil {
			return err
		}
	}
	return nil
}

func (w *JSONLWriter) rotateIfNeededLocked(incomingBytes int64) error {
	if w.opts.RotateMaxBytes <= 0 {
		return nil
	}
	if w.size+incomingBytes <= w.opts.RotateMaxBytes {
		return nil
	}
	if w.writer != nil {
		_ = w.writer.Flush()
	}
	if w.file != nil {
		_ = w.file.Close()
	}

	if err := w.renameCurrentWithTimestampLocked(); err != nil {
		return err
	}
	w.file = nil
	w.writer = nil
	w.size = 0
	return w.openLocked()
}

func (w *JSONLWriter) renameCurrentWithTimestampLocked() error {
	ts := w.now().UTC().Format("20060102T150405Z")
	base := fmt.Sprintf("%s.%s", w.path, ts)
	rotatedPath := base
	for i := 0; ; i++ {
		if i > 0 {
			rotatedPath = fmt.Sprintf("%s.%d", base, i)
		}
		if _, err := os.Stat(rotatedPath); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(w.path, rotatedPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		return nil
	}
}

func (w *JSONLWriter) openLocked() error {
	if err := EnsureDir(filepath.Dir(w.path), w.opts.DirPerm); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, w.opts.FilePerm)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.writer = bufio.NewWriterSize(file, 64*1024)
	w.size = info.Size()
	return nil
}
