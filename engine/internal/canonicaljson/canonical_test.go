package canonicaljson

import "testing"

func TestMarshalSortsKeysAndPreservesInteger(t *testing.T) {
	got, err := Marshal(map[string]any{"z": 2, "a": "<safe>"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":"<safe>","z":2}`
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestMarshalUsesUTF16PropertyOrder(t *testing.T) {
	got, err := Marshal(map[string]any{"😀": 1, "€": 2, "1": 3, "ö": 4})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"1":3,"ö":4,"€":2,"😀":1}`
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestMarshalRejectsNonIntegerProtocolNumber(t *testing.T) {
	if _, err := Marshal(map[string]any{"value": 1.5}); err == nil {
		t.Fatal("expected non-integer rejection")
	}
}
