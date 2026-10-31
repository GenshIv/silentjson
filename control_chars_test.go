package silentjson

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// TestMarshalString_ControlCharacters guards the AVX2 appendString fast path:
// control characters (< 0x20) inside strings must be escaped exactly like
// encoding/json does. The vectorized scanner used to look only for '"' and
// '\', so raw \n / \t / \x00.. leaked into the output and produced invalid
// JSON that browsers refuse to parse.
func TestMarshalString_ControlCharacters(t *testing.T) {
	type doc struct {
		Desc string `json:"desc"`
	}
	reg := BuildRegistry(reflect.TypeOf(doc{}))

	cases := []string{
		"",
		"plain ascii",
		"line1\nline2",
		"cr\rreturn",
		"tab\there",
		"backspace \b and formfeed \f",
		"\x00\x01\x0b\x1f",
		"nul\x00in the middle",
		`quote"and\backslash`,
		`mixed "\ ` + "\n\t\x01",
		strings.Repeat("a", 31) + "\n",  // newline just past AVX2 chunk boundary
		strings.Repeat("b", 32) + "\n",  // newline in the second chunk
		strings.Repeat("c", 33) + "\r\n\t",
		strings.Repeat(`d"e\`, 20) + "\x1f",
		"unicode ąćę 🎉 stays intact",
	}

	for _, s := range cases {
		v := doc{Desc: s}
		got := Marshal(&v, reg, nil)
		want, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("encoding/json failed for %q: %v", s, err)
		}
		// silentjson escapes control chars as uppercase \uXXXX, encoding/json
		// as lowercase — both are valid JSON. Compare semantically.
		var gotVal, wantVal interface{}
		if err := json.Unmarshal(got, &gotVal); err != nil {
			t.Errorf("output for %q is not valid JSON: %v (got %q)", s, err, got)
			continue
		}
		if err := json.Unmarshal(want, &wantVal); err != nil {
			t.Fatalf("encoding/json produced invalid JSON: %v", err)
		}
		if !reflect.DeepEqual(gotVal, wantVal) {
			t.Errorf("input %q: semantic mismatch:\n got: %q\nwant: %q", s, got, want)
		}
		// The output must be parseable JSON — the original symptom was
		// browsers failing with "Bad control character in string literal".
		var back doc
		if err := json.Unmarshal(got, &back); err != nil {
			t.Errorf("output for %q is not valid JSON: %v (got %q)", s, err, got)
		}
		if back.Desc != s {
			t.Errorf("round-trip mismatch for %q: got %q", s, back.Desc)
		}
	}
}

// TestMarshalString_ControlCharacters_LongStrings covers strings long enough
// to exercise multiple 32-byte AVX2 chunks with control chars in each part.
func TestMarshalString_ControlCharacters_LongStrings(t *testing.T) {
	type doc struct {
		Desc string `json:"desc"`
	}
	reg := BuildRegistry(reflect.TypeOf(doc{}))

	prefix := strings.Repeat("x", 100)
	mid := strings.Repeat("y", 100)
	suffix := strings.Repeat("z", 100)
	s := prefix + "\n" + mid + "\t" + suffix + "\x01"

	v := doc{Desc: s}
	got := Marshal(&v, reg, nil)
	var gotVal interface{}
	if err := json.Unmarshal(got, &gotVal); err != nil {
		t.Fatalf("output is not valid JSON: %v (got %q)", err, got)
	}
	if gotVal.(map[string]interface{})["desc"] != s {
		t.Errorf("long string round-trip mismatch:\n got: %q", got)
	}
}

// TestAppendStringASM_Contract verifies the direct (buf, specialPos) contract
// of appendStringASM: control characters must yield specialPos >= 0 so the
// Go slow path escapes them, and plain strings must come back complete with
// specialPos == -1.
func TestAppendStringASM_Contract(t *testing.T) {
	tests := []struct {
		in        string
		wantSpecial bool // expect specialPos >= 0
	}{
		{"plain", false},
		{`has "quote"`, false}, // quotes are escaped inline; specialPos stays -1
		{`has \backslash`, false},
		{"raw\nnewline", true},
		{"raw\ttab", true},
		{"\x00", true},
		{"\x1f", true},
		{"lead\x0bctrl", true},
	}
	for _, tc := range tests {
		buf := make([]byte, 0, 4096)
		out, specialPos := appendStringASM(buf, tc.in)
		if tc.wantSpecial {
			if specialPos < 0 {
				t.Errorf("input %q: expected specialPos >= 0, got %d", tc.in, specialPos)
			}
			continue
		}
		if specialPos != -1 {
			t.Errorf("input %q: expected specialPos -1, got %d", tc.in, specialPos)
		}
		if !bytes.Equal(out, []byte(`"`+strings.ReplaceAll(strings.ReplaceAll(tc.in, `\`, `\\`), `"`, `\"`)+`"`)) {
			t.Errorf("input %q: unexpected inline-escaped output %q", tc.in, out)
		}
	}
	_ = unsafe.Pointer(nil)
}
