//go:build unix

package apply

import (
	"errors"
	"syscall"
)

// isUnsupportedLinkError reports whether a hard-link failure means the target
// filesystem cannot create hard links. Linux returns EPERM for filesystems
// such as vfat; the temporary file is always created by this run, so the
// protected_hardlinks ownership check does not apply here. macOS and BSDs use
// ENOTSUP/EOPNOTSUPP, which differ numerically on some of them.
func isUnsupportedLinkError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	// ENOTSUP and EOPNOTSUPP share a value on Linux, so they cannot both be
	// switch cases.
	return errno == syscall.EPERM || errno == syscall.ENOTSUP || errno == syscall.EOPNOTSUPP
}
