//go:build unix

package apply

import (
	"errors"
	"syscall"
)

// isUnsupportedLinkError reports whether a hard-link failure means the target
// filesystem cannot create hard links. EPERM is ambiguous: mount policies or
// security controls can deny linking even when the filesystem supports it.
// Keep it as an ordinary access error rather than claiming lack of support.
func isUnsupportedLinkError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	// ENOTSUP and EOPNOTSUPP share a value on Linux, so they cannot both be
	// switch cases.
	return errno == syscall.ENOTSUP || errno == syscall.EOPNOTSUPP
}
