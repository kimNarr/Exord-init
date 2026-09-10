package canonicaljson

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestMarshalRejectsIntegerOutsideSigned64Bit(t *testing.T) {
	if _, err := Marshal(map[string]any{"value": json.Number("9223372036854775808")}); err == nil {
		t.Fatal("expected out-of-range integer rejection")
	}
}

// TestMarshalStringEscapingIsValidAndRoundTrips guards the RFC 8785 string
// serialization against the earlier defect where literal backslash-u sequences
// and real separators were rewritten into invalid JSON escapes.
func TestMarshalStringEscapingIsValidAndRoundTrips(t *testing.T) {
	cases := map[string]string{
		"literal-lt-escape":        `prefix \u003c suffix`,
		"literal-line-separator":   `\u2028`,
		"windows-path-with-lt":     `C:\users\u003cfoo`,
		"real-html-chars":          "a < b > c & d",
		"real-unicode-separators":  "before\u2028middle\u2029after",
		"quote-backslash-controls": "quote \" backslash \\ bell \b form \f null \x00 unit \x1f",
		"tab-newline-return":       "tab\tnewline\nreturn\r",
		"astral-plane":             "emoji 😀 and CJK 한국어",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := Marshal(map[string]any{"k": input})
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			if !json.Valid(out) {
				t.Fatalf("canonical output is not valid JSON: %s", out)
			}
			var back map[string]string
			if err := json.NewDecoder(strings.NewReader(string(out))).Decode(&back); err != nil {
				t.Fatalf("re-decode with encoding/json failed: %v (output %s)", err, out)
			}
			if back["k"] != input {
				t.Fatalf("round-trip mismatch: got %q want %q (output %s)", back["k"], input, out)
			}
		})
	}
}

func TestMarshalStringEscapesExactlyLikeRFC8785(t *testing.T) {
	got, err := Marshal(map[string]any{"k": "< > & \u2028 \" \\ \b \f \n \r \t \x01"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"k":"< > & ` + "\u2028" + ` \" \\ \b \f \n \r \t \u0001"}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestMarshalKeysWithControlCharactersStayValid(t *testing.T) {
	out, err := Marshal(map[string]any{"tab\tkey": 1, "plain": 2})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("canonical output with control-character key is invalid: %s", out)
	}
	var back map[string]int
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}
	if back["tab\tkey"] != 1 || back["plain"] != 2 {
		t.Fatalf("unexpected decoded map: %#v", back)
	}
}
