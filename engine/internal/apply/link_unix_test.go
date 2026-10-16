//go:build unix

package apply

import (
	"os"
	"syscall"
	"testing"
)

func TestUnixLinkErrorsDistinguishSupportFromPermissions(t *testing.T) {
	for _, tc := range []struct {
		err         syscall.Errno
		unsupported bool
	}{
		{syscall.ENOTSUP, true},
		{syscall.EOPNOTSUPP, true},
		{syscall.EPERM, false},
		{syscall.EACCES, false},
		{syscall.EXDEV, false},
	} {
		err := &os.LinkError{Op: "link", Old: "temp", New: "destination", Err: tc.err}
		if got := isUnsupportedLinkError(err); got != tc.unsupported {
			t.Errorf("errno %v: unsupported = %v, want %v", tc.err, got, tc.unsupported)
		}
	}
}
