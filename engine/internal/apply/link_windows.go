//go:build windows

package apply

import (
	"errors"
	"syscall"
)

const (
	errorInvalidFunction syscall.Errno = 1  // ERROR_INVALID_FUNCTION
	errorNotSupported    syscall.Errno = 50 // ERROR_NOT_SUPPORTED
)

// isUnsupportedLinkError reports whether a hard-link failure means the target
// volume cannot create hard links (for example FAT32/exFAT or some network
// shares), as opposed to an ordinary I/O or access error.
func isUnsupportedLinkError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case errorInvalidFunction, errorNotSupported:
		return true
	}
	return false
}
