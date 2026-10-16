package apply

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestIsUnsupportedLinkErrorIgnoresOrdinaryFailures(t *testing.T) {
	for _, err := range []error{
		errors.New("plain failure"),
		&os.LinkError{Op: "link", Old: "a", New: "b", Err: os.ErrExist},
		fmt.Errorf("wrapped: %w", os.ErrNotExist),
	} {
		if isUnsupportedLinkError(err) {
			t.Fatalf("ordinary failure misclassified as unsupported hard link: %v", err)
		}
	}
}
