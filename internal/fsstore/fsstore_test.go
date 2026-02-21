package fsstore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildLockPath(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".fslocks")
	got, err := BuildLockPath(root, "state.main")
	if err != nil {
		t.Fatalf("BuildLockPath() error = %v", err)
	}
	want := filepath.Join(root, "state.main.lck")
	if got != want {
		t.Fatalf("BuildLockPath() = %q, want %q", got, want)
	}
}

func TestBuildLockPathInvalidKey(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".fslocks")
	invalid := []string{
		"",
		"State.main",
		"state/main",
		".state.main",
		"state.main.",
		"state main",
	}
	for _, key := range invalid {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			_, err := BuildLockPath(root, key)
			if err == nil {
				t.Fatalf("BuildLockPath(%q) expected error", key)
			}
			if !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("BuildLockPath(%q) error = %v, want ErrInvalidPath", key, err)
			}
		})
	}
}

func TestReadWriteJSONAtomic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	type payload struct {
		Name string `json:"name"`
	}
	in := payload{Name: "alpha"}
	if err := WriteJSONAtomic(path, in, FileOptions{}); err != nil {
		t.Fatalf("WriteJSONAtomic() error = %v", err)
	}
	var out payload
	ok, err := ReadJSON(path, &out)
	if err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}
	if !ok {
		t.Fatalf("ReadJSON() exists = false, want true")
	}
	if out.Name != in.Name {
		t.Fatalf("ReadJSON() value = %+v, want %+v", out, in)
	}
}

func TestReadJSONStrictRejectsUnknownField(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{"name":"alpha","unknown":"x"}` + "\n"
	if err := WriteTextAtomic(path, raw, FileOptions{}); err != nil {
		t.Fatalf("WriteTextAtomic() error = %v", err)
	}
	var out struct {
		Name string `json:"name"`
	}
	_, err := ReadJSONStrict(path, &out)
	if err == nil {
		t.Fatalf("ReadJSONStrict() expected decode error")
	}
	if !errors.Is(err, ErrDecodeFailed) {
		t.Fatalf("ReadJSONStrict() error = %v, want ErrDecodeFailed", err)
	}
}

func TestReadJSONStrictRejectsTrailingData(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{"name":"alpha"}` + "\n" + `{"name":"beta"}` + "\n"
	if err := WriteTextAtomic(path, raw, FileOptions{}); err != nil {
		t.Fatalf("WriteTextAtomic() error = %v", err)
	}
	var out struct {
		Name string `json:"name"`
	}
	_, err := ReadJSONStrict(path, &out)
	if err == nil {
		t.Fatalf("ReadJSONStrict() expected trailing data error")
	}
	if !errors.Is(err, ErrDecodeFailed) {
		t.Fatalf("ReadJSONStrict() error = %v, want ErrDecodeFailed", err)
	}
}

func TestReadWriteTextAtomic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "notes.md")
	in := "hello\nworld\n"
	if err := WriteTextAtomic(path, in, FileOptions{}); err != nil {
		t.Fatalf("WriteTextAtomic() error = %v", err)
	}
	got, ok, err := ReadText(path)
	if err != nil {
		t.Fatalf("ReadText() error = %v", err)
	}
	if !ok {
		t.Fatalf("ReadText() exists = false, want true")
	}
	if got != in {
		t.Fatalf("ReadText() = %q, want %q", got, in)
	}
}

func TestMutateIndex(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	indexPath := filepath.Join(root, "contacts", "index.json")
	lockPath, err := BuildLockPath(filepath.Join(root, ".fslocks"), "index.contacts")
	if err != nil {
		t.Fatalf("BuildLockPath() error = %v", err)
	}

	err = MutateIndex(context.Background(), indexPath, lockPath, FileOptions{}, func(f *IndexFile) error {
		f.Entries["agent:abc"] = IndexEntry{
			Ref:       "contacts/active.md#agent-abc",
			Rev:       1,
			Hash:      "sha256:abc",
			UpdatedAt: time.Now().UTC(),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("MutateIndex() error = %v", err)
	}

	f, ok, err := ReadIndex(indexPath)
	if err != nil {
		t.Fatalf("ReadIndex() error = %v", err)
	}
	if !ok {
		t.Fatalf("ReadIndex() exists = false, want true")
	}
	if f.Version != defaultIndexVersion {
		t.Fatalf("ReadIndex() version = %d, want %d", f.Version, defaultIndexVersion)
	}
	if _, exists := f.Entries["agent:abc"]; !exists {
		t.Fatalf("ReadIndex() missing entry agent:abc")
	}
}

func TestJSONLWriterRotateCollision(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "audit.jsonl")
	w, err := NewJSONLWriter(path, JSONLOptions{
		RotateMaxBytes: 10,
		FlushEachWrite: true,
	})
	if err != nil {
		t.Fatalf("NewJSONLWriter() error = %v", err)
	}
	defer w.Close()

	fixed := time.Date(2026, 2, 7, 8, 0, 1, 0, time.UTC)
	w.now = func() time.Time { return fixed }

	baseRotated := path + "." + fixed.Format("20060102T150405Z")
	if err := WriteTextAtomic(baseRotated, "old\n", FileOptions{}); err != nil {
		t.Fatalf("WriteTextAtomic(baseRotated) error = %v", err)
	}

	if err := w.AppendLine("line-1"); err != nil {
		t.Fatalf("AppendLine(line-1) error = %v", err)
	}
	if err := w.AppendLine("line-2"); err != nil {
		t.Fatalf("AppendLine(line-2) error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	rotatedWithSuffix := baseRotated + ".1"
	content, ok, err := ReadText(rotatedWithSuffix)
	if err != nil {
		t.Fatalf("ReadText(rotatedWithSuffix) error = %v", err)
	}
	if !ok {
		t.Fatalf("ReadText(rotatedWithSuffix) exists = false, want true")
	}
	if !strings.Contains(content, "line-1") {
		t.Fatalf("rotated file content = %q, want to contain line-1", content)
	}
}
