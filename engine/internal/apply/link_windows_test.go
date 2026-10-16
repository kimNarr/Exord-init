//go:build windows

package apply

import (
	"os"
	"syscall"
	"testing"
)

func TestWindowsLinkErrorsDistinguishSupportFromPermissions(t *testing.T) {
	for _, tc := range []struct {
		err         syscall.Errno
		unsupported bool
	}{
		{errorInvalidFunction, true},
		{errorNotSupported, true},
		{syscall.ERROR_ACCESS_DENIED, false},
		{syscall.ERROR_FILE_EXISTS, false},
	} {
		err := &os.LinkError{Op: "link", Old: "temp", New: "destination", Err: tc.err}
		if got := isUnsupportedLinkError(err); got != tc.unsupported {
			t.Errorf("errno %v: unsupported = %v, want %v", tc.err, got, tc.unsupported)
		}
	}
}
