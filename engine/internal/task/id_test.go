package task

import (
	"bytes"
	"testing"
	"time"
)

func TestUniqueIDRetriesAndUsesUTCDate(t *testing.T) {
	now := time.Date(2026, 10, 16, 1, 0, 0, 0, time.FixedZone("test", 9*60*60))
	used := map[string]bool{"T-20261015-00000000": true}
	source := append(make([]byte, 8), bytes.Repeat([]byte{1}, 8)...)
	value, err := uniqueID(now, used, bytes.NewReader(source))
	if err != nil || value != "T-20261015-11111111" || !ValidID(value) {
		t.Fatalf("ID=%s %v", value, err)
	}
	if _, err := uniqueID(now, used, bytes.NewReader(make([]byte, 32*8))); err == nil {
		t.Fatal("unbounded collision retries")
	}
	if _, err := uniqueID(now, nil, bytes.NewReader(nil)); err == nil {
		t.Fatal("entropy failure ignored")
	}
}

func TestCollectIDsFromBranchesAndCompletionMessages(t *testing.T) {
	used := map[string]bool{}
	CollectIDs([]byte("feat/t-20261016-7k3m2q9r-login\nTask: T-20261015-00000000"), used)
	if !used["T-20261016-7K3M2Q9R"] || !used["T-20261015-00000000"] {
		t.Fatal(used)
	}
}
