package fsstore

import "os"

const (
	defaultDirPerm        = 0o700
	defaultFilePerm       = 0o600
	defaultRotateMaxBytes = 100 * 1024 * 1024
)

type FileOptions struct {
	DirPerm  os.FileMode
	FilePerm os.FileMode
}

type JSONLOptions struct {
	DirPerm        os.FileMode
	FilePerm       os.FileMode
	RotateMaxBytes int64
	FlushEachWrite bool
	SyncEachWrite  bool
}

func normalizeFileOptions(opts FileOptions) FileOptions {
	if opts.DirPerm == 0 {
		opts.DirPerm = defaultDirPerm
	}
	if opts.FilePerm == 0 {
		opts.FilePerm = defaultFilePerm
	}
	return opts
}

func normalizeJSONLOptions(opts JSONLOptions) JSONLOptions {
	if opts.DirPerm == 0 {
		opts.DirPerm = defaultDirPerm
	}
	if opts.FilePerm == 0 {
		opts.FilePerm = defaultFilePerm
	}
	if opts.RotateMaxBytes <= 0 {
		opts.RotateMaxBytes = defaultRotateMaxBytes
	}
	if !opts.FlushEachWrite && !opts.SyncEachWrite {
		// Default to flushing each write for predictable behavior.
		opts.FlushEachWrite = true
	}
	return opts
}
