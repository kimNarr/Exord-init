package canonicaljson

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"unicode/utf16"
)

// Marshal implements the RFC 8785 rules needed by the v1 protocol. Protocol
// numbers are integers, so non-integer JSON numbers are rejected explicitly.
func Marshal(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var generic any
	if err := decoder.Decode(&generic); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := write(&out, generic); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func write(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(v))
	case string:
		encoded, _ := json.Marshal(v)
		encoded = bytes.ReplaceAll(encoded, []byte(`\u003c`), []byte("<"))
		encoded = bytes.ReplaceAll(encoded, []byte(`\u003e`), []byte(">"))
		encoded = bytes.ReplaceAll(encoded, []byte(`\u0026`), []byte("&"))
		encoded = bytes.ReplaceAll(encoded, []byte(`\u2028`), []byte("\xe2\x80\xa8"))
		encoded = bytes.ReplaceAll(encoded, []byte(`\u2029`), []byte("\xe2\x80\xa9"))
		out.Write(encoded)
	case json.Number:
		if _, err := strconv.ParseInt(v.String(), 10, 64); err != nil {
			return errors.New("canonical JSON protocol only accepts signed 64-bit integers")
		}
		out.WriteString(v.String())
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := write(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := write(out, key); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := write(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return errors.New("unsupported canonical JSON value")
	}
	return nil
}

func utf16Less(a, b string) bool {
	aa, bb := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(aa) && i < len(bb); i++ {
		if aa[i] != bb[i] {
			return aa[i] < bb[i]
		}
	}
	return len(aa) < len(bb)
}
