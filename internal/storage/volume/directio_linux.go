//go:build linux

package volume

import (
	"os"
	"syscall"
)

// directIOSupported returns true on Linux where O_DIRECT is available.
func directIOSupported() bool {
	return true
}

// OpenFileDirect opens a file with O_DIRECT flag for direct I/O.
func OpenFileDirect(path string, flag int, perm os.FileMode) (*os.File, error) {
	// Add O_DIRECT to the flags
	return os.OpenFile(path, flag|syscall.O_DIRECT, perm)
}

// CreateFileDirect creates a file with O_DIRECT flag for direct I/O.
func CreateFileDirect(path string, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_DIRECT, perm)
}

// Fallocate pre-allocates disk space for a file
// This is more efficient than Truncate as it actually allocates disk blocks.
func Fallocate(file *os.File, size int64) error {
	// mode 0 = default allocation
	return syscall.Fallocate(int(file.Fd()), 0, 0, size)
}

// Fdatasync syncs file data to disk (without metadata)
// This is faster than Sync() when you only care about data.
func Fdatasync(file *os.File) error {
	return syscall.Fdatasync(int(file.Fd()))
}

// PunchHole deallocates disk space for a range in the file
// This is useful for sparse files and reclaiming space.
func PunchHole(file *os.File, offset, length int64) error {
	// FALLOC_FL_PUNCH_HOLE | FALLOC_FL_KEEP_SIZE
	const (
		FALLOC_FL_PUNCH_HOLE = 0x02
		FALLOC_FL_KEEP_SIZE  = 0x01
	)

	return syscall.Fallocate(int(file.Fd()), FALLOC_FL_PUNCH_HOLE|FALLOC_FL_KEEP_SIZE, offset, length)
}

// ZeroRange zeros a range in the file without writing zeros
// More efficient than writing zeros manually.
func ZeroRange(file *os.File, offset, length int64) error {
	// FALLOC_FL_ZERO_RANGE | FALLOC_FL_KEEP_SIZE
	const (
		FALLOC_FL_ZERO_RANGE = 0x10
		FALLOC_FL_KEEP_SIZE  = 0x01
	)

	return syscall.Fallocate(int(file.Fd()), FALLOC_FL_ZERO_RANGE|FALLOC_FL_KEEP_SIZE, offset, length)
}

// Fadvise advises the kernel about access patterns
// This can improve performance by hinting sequential or random access.
func Fadvise(file *os.File, offset, length int64, advice int) error {
	_, _, errno := syscall.Syscall6(
		syscall.SYS_FADVISE64,
		uintptr(file.Fd()),
		uintptr(offset),
		uintptr(length),
		uintptr(advice),
		0, 0,
	)
	if errno != 0 {
		return errno
	}

	return nil
}

// Fadvise constants.
const (
	FADV_NORMAL     = 0 // No special treatment
	FADV_RANDOM     = 1 // Expect random page references
	FADV_SEQUENTIAL = 2 // Expect sequential page references
	FADV_WILLNEED   = 3 // Will need these pages soon
	FADV_DONTNEED   = 4 // Don't need these pages anymore
	FADV_NOREUSE    = 5 // Data will only be accessed once
)
