package silentjson

import (
	"testing"
)

// TestParseShortStringEscapes exercises the escaped-string path that calls allocBytes.
// Input should be the string content AFTER the opening quote, ending with the closing quote.
func TestParseShortStringEscapes(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"minimal_escape", `a\n"`, "a\n"},
		{"single_escape", `hello\nworld"`, "hello\nworld"},
		{"escaped_quote", `say \"hi\""`, `say "hi"`},
		{"escaped_backslash", `path\\to\\file"`, `path\to\file`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.input)
			// Call AVX2 implementation directly to isolate the issue
			result, consumed := parseShortStringAVX2(data)
			if string(result) != tc.expected {
				t.Errorf("parseShortStringAVX2(%q) = %q (len=%d), want %q", tc.input, result, len(result), tc.expected)
			}
			if consumed != int64(len(tc.input)) {
				t.Errorf("consumed = %d, want %d", consumed, len(tc.input))
			}
		})
	}
}
