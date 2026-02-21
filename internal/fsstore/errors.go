package fsstore

import "errors"

var (
	ErrInvalidPath       = errors.New("fsstore: invalid path")
	ErrLockTimeout       = errors.New("fsstore: lock timeout")
	ErrLockUnavailable   = errors.New("fsstore: lock unavailable")
	ErrEncodeFailed      = errors.New("fsstore: encode failed")
	ErrDecodeFailed      = errors.New("fsstore: decode failed")
	ErrAtomicWriteFailed = errors.New("fsstore: atomic write failed")
)
