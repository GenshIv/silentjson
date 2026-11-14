//go:build amd64

package silentjson

import (
	"testing"
)

func TestParseJSONStringComplete(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", `""`, ""},
		{"plain", `"hello"`, "hello"},
		{"newline", `"line1\nline2"`, "line1\nline2"},
		{"tab", `"a\tb"`, "a\tb"},
		{"quote", `"say \"hi\""`, `say "hi"`},
		{"backslash", `"path\\to\\file"`, "path\\to\\file"},
		{"unicode ascii", `"\u0041"`, "A"},
		{"unicode emoji", `"\u2764"`, "❤"},
		{"mixed", `"Hello \u0041\u0042\u0043!"`, "Hello ABC!"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.input[1:]) // Remove opening quote
			dst := make([]byte, len(src))

			written, consumed := parseJSONStringCompleteASM(src, dst)
			if written < 0 {
				t.Fatalf("parse failed: written=%d, consumed=%d", written, consumed)
			}

			result := string(dst[:written])
			expected := tc.expected
			if result != expected {
				t.Errorf("got %q, want %q", result, expected)
			}
		})
	}
}
