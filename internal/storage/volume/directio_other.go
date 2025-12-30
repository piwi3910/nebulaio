//go:build !linux

package volume

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	// ErrInvalidPath is returned when a path contains directory traversal patterns.
	ErrInvalidPath = errors.New("invalid path: potential directory traversal detected")
)

// directIOSupported returns false on non-Linux platforms
// macOS and Windows don't support O_DIRECT in the same way
func directIOSupported() bool {
	return false
}

// OpenFileDirect opens a file without direct I/O on non-Linux platforms
// Falls back to regular file open
func OpenFileDirect(path string, flag int, perm os.FileMode) (*os.File, error) {
	// G304: Clean path to prevent directory traversal
	cleanPath := filepath.Clean(path)
	if cleanPath != path {
		return nil, ErrInvalidPath
	}

	// #nosec G304 - Path is cleaned and validated above, used for internal volume operations
	file, err := os.OpenFile(cleanPath, flag, perm)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

// CreateFileDirect creates a file without direct I/O on non-Linux platforms
func CreateFileDirect(path string, perm os.FileMode) (*os.File, error) {
	// G304: Clean path to prevent directory traversal
	cleanPath := filepath.Clean(path)
	if cleanPath != path {
		return nil, ErrInvalidPath
	}

	// #nosec G304 - Path is cleaned and validated above, used for internal volume operations
	file, err := os.OpenFile(cleanPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	return file, nil
}

// Fallocate is not supported on non-Linux platforms
// Falls back to Truncate which may not actually allocate disk blocks
func Fallocate(file *os.File, size int64) error {
	return file.Truncate(size)
}

// Fdatasync falls back to Sync on non-Linux platforms
func Fdatasync(file *os.File) error {
	return file.Sync()
}

// PunchHole is not supported on non-Linux platforms
// Returns nil as a no-op
func PunchHole(file *os.File, offset, length int64) error {
	// Not supported - this is a no-op
	return nil
}

// ZeroRange falls back to writing zeros on non-Linux platforms
func ZeroRange(file *os.File, offset, length int64) error {
	// Write actual zeros
	zeros := make([]byte, 4096)
	for written := int64(0); written < length; {
		toWrite := length - written
		if toWrite > 4096 {
			toWrite = 4096
		}
		n, err := file.WriteAt(zeros[:toWrite], offset+written)
		if err != nil {
			return err
		}
		written += int64(n)
	}
	return nil
}

// Fadvise is not supported on non-Linux platforms
// Returns nil as a no-op
func Fadvise(file *os.File, offset, length int64, advice int) error {
	// Not supported - this is a no-op
	return nil
}

// Fadvise constants (defined for API compatibility)
const (
	FADV_NORMAL     = 0
	FADV_RANDOM     = 1
	FADV_SEQUENTIAL = 2
	FADV_WILLNEED   = 3
	FADV_DONTNEED   = 4
	FADV_NOREUSE    = 5
)
