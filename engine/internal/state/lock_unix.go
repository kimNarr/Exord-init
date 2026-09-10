//go:build unix

package state

import "syscall"

// acquireOSLock takes a non-blocking exclusive advisory lock (flock) on the
// open lock file. The kernel drops the lock automatically when the file
// description is closed, including on process exit, so an interrupted run can
// never orphan the lock. errLockHeld means another live process holds it.
func acquireOSLock(fd uintptr) error {
	err := syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK {
		return errLockHeld
	}
	return err
}

func releaseOSLock(fd uintptr) {
	_ = syscall.Flock(int(fd), syscall.LOCK_UN)
}
