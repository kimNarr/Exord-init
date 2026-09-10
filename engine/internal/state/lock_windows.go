//go:build windows

package state

import (
	"syscall"
	"unsafe"
)

// The Windows equivalent of flock is LockFileEx on the open handle. Windows
// releases the byte-range lock when the handle closes or the process exits,
// giving the same crash-safe behavior as the unix path. This mirrors the
// approach used by the Go toolchain's own lockedfile package and needs no
// third-party dependency.
var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	errorLockViolation      = syscall.Errno(0x21)
)

func acquireOSLock(fd uintptr) error {
	overlapped := new(syscall.Overlapped)
	r1, _, err := procLockFileEx.Call(
		fd,
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0, 1, 0,
		uintptr(unsafe.Pointer(overlapped)),
	)
	if r1 != 0 {
		return nil
	}
	if errno, ok := err.(syscall.Errno); ok && (errno == errorLockViolation || errno == syscall.ERROR_IO_PENDING) {
		return errLockHeld
	}
	return err
}

func releaseOSLock(fd uintptr) {
	overlapped := new(syscall.Overlapped)
	_, _, _ = procUnlockFileEx.Call(fd, 0, 1, 0, uintptr(unsafe.Pointer(overlapped)))
}
