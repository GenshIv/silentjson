package silentjson

import (
	"testing"
)

// TestParseShortStringScalarEscapes tests the scalar implementation directly.
func TestParseShortStringScalarEscapes(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"single_escape", `hello\nworld"`, "hello\nworld"},
		{"escaped_quote", `say \"hi\""`, `say "hi"`},
		{"escaped_backslash", `path\\to\\file"`, `path\to\file`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.input)
			result, consumed := parseShortStringScalar(data)
			if string(result) != tc.expected {
				t.Errorf("parseShortStringScalar(%q) = %q, want %q", tc.input, result, tc.expected)
			}
			if consumed != int64(len(tc.input)) {
				t.Errorf("consumed = %d, want %d", consumed, len(tc.input))
			}
		})
	}
}
