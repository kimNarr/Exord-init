package main

import "testing"

func TestFailureRedactsCause(t *testing.T) {
	result := failure("plan", "UNSUPPORTED_TARGET", "error.target_unsafe", secretError("C:\\private\\secret.txt"))
	if len(result.Warnings) != 0 {
		t.Fatalf("cause leaked into result: %v", result.Warnings)
	}
}

type secretError string

func (e secretError) Error() string { return string(e) }
