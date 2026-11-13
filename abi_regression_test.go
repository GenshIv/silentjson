package silentjson

import (
	"fmt"
	"testing"
)

// TestParseShortStringAVX2Escapes verifies the escaped-string path in parseShortStringAVX2.
// This exercises the allocBytes call and register preservation across the ABI0 boundary.
func TestParseShortStringAVX2Escapes(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
		consumed int64
	}{
		// Single escape sequences
		{"newline", `a\n"`, "a\n", 4},
		{"carriage_return", `a\r"`, "a\r", 4},
		{"tab", `a\t"`, "a\t", 4},
		{"backspace", `a\b"`, "a\b", 4},
		{"form_feed", `a\f"`, "a\f", 4},
		{"escaped_quote", `say \"hi\""`, `say "hi"`, 11},
		{"escaped_backslash", `path\\to\\file"`, `path\to\file`, 15},
		{"escaped_slash", `a\/b"`, `a/b`, 5},

		// Multiple escapes
		{"two_escapes", `a\nb\tc"`, "a\nb\tc", 8},
		{"three_escapes", `a\nb\r\nc"`, "a\nb\r\nc", 10},
		{"many_escapes", `\n\n\n\n\n"`, "\n\n\n\n\n", 11},

		// Empty and minimal
		{"empty_string", `"`, "", 1},
		{"single_char", `x"`, "x", 2},
		{"minimal_escape", `\n"`, "\n", 3},

		// Escapes in middle
		{"escape_middle", `hello\nworld"`, "hello\nworld", 13},
		{"escape_near_end", `abc\nd"`, "abc\nd", 7},

		// No escapes (should take fast path)
		{"no_escapes", `plain text"`, "plain text", 11},
		{"unicode_no_escape", `héllo wörld"`, "héllo wörld", 14},

		// Boundary cases near SIMD block size (32 bytes)
		{"near_simd_boundary", `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"`, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", 34},
		{"past_simd_boundary", `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"`, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", 37},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.input)
			result, consumed := parseShortStringAVX2(data)
			if string(result) != tc.expected {
				t.Errorf("parseShortStringAVX2(%q) = %q (len=%d), want %q", tc.input, result, len(result), tc.expected)
			}
			if consumed != tc.consumed {
				t.Errorf("consumed = %d, want %d", consumed, tc.consumed)
			}
		})
	}
}

// TestParseShortStringAVX2VsScalar verifies AVX2 and scalar implementations agree.
func TestParseShortStringAVX2VsScalar(t *testing.T) {
	testCases := []string{
		`hello"`,
		`hello\nworld"`,
		`"`,
		`\n"`,
		`a\"b"`,
		`path\\to\\file"`,
		`line1\nline2\r\nline3\ttab"`,
		`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, // 33 chars, past SIMD block
		`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"`, // escape past SIMD block
	}

	for _, input := range testCases {
		data := []byte(input)
		avx2Result, avx2Consumed := parseShortStringAVX2(data)
		scalarResult, scalarConsumed := parseShortStringScalar(data)

		if string(avx2Result) != string(scalarResult) {
			t.Errorf("AVX2 and scalar disagree on %q:\n  AVX2: %q (consumed=%d)\n  scalar: %q (consumed=%d)",
				input, avx2Result, avx2Consumed, scalarResult, scalarConsumed)
		}
		if avx2Consumed != scalarConsumed {
			t.Errorf("AVX2 and scalar disagree on consumed for %q: AVX2=%d, scalar=%d",
				input, avx2Consumed, scalarConsumed)
		}
	}
}

// TestParseShortStringAVX2EOF verifies EOF handling.
func TestParseShortStringAVX2EOF(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{"no_closing_quote", `unclosed string`},
		{"backslash_at_eof", `trailing backslash\`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.input)
			result, consumed := parseShortStringAVX2(data)
			if result != nil && len(result) > 0 {
				t.Errorf("expected empty result for EOF case %q, got %q", tc.input, result)
			}
			if consumed != -1 {
				t.Errorf("expected consumed=-1 for EOF case %q, got %d", tc.input, consumed)
			}
		})
	}
}

// TestParseShortStringAVX2LongStrings tests strings that span multiple SIMD blocks.
func TestParseShortStringAVX2LongStrings(t *testing.T) {
	// Build a long string with escapes at various positions
	longInput := make([]byte, 0, 128)
	for i := 0; i < 64; i++ {
		longInput = append(longInput, 'a'+byte(i%26))
	}
	longInput = append(longInput, '\\', 'n') // escape at position 64
	for i := 0; i < 32; i++ {
		longInput = append(longInput, 'b'+byte(i%26))
	}
	longInput = append(longInput, '"')

	expected := make([]byte, 0, 95)
	for i := 0; i < 64; i++ {
		expected = append(expected, 'a'+byte(i%26))
	}
	expected = append(expected, '\n')
	for i := 0; i < 32; i++ {
		expected = append(expected, 'b'+byte(i%26))
	}

	result, consumed := parseShortStringAVX2(longInput)
	if string(result) != string(expected) {
		t.Errorf("long string test failed:\n  got: %q\n  want: %q", result, expected)
	}
	if consumed != int64(len(longInput)) {
		t.Errorf("consumed = %d, want %d", consumed, len(longInput))
	}
}

// TestParseShortStringAVX2EscapeAtBoundaries tests escapes at SIMD block boundaries.
func TestParseShortStringAVX2EscapeAtBoundaries(t *testing.T) {
	// Test escape at positions 31, 32, 33 (around first SIMD block boundary)
	for offset := 30; offset <= 34; offset++ {
		t.Run(fmt.Sprintf("escape_at_%d", offset), func(t *testing.T) {
			input := make([]byte, 0, offset+4)
			for i := 0; i < offset; i++ {
				input = append(input, 'x')
			}
			input = append(input, '\\', 'n')
			input = append(input, 'y')
			input = append(input, '"')

			expected := make([]byte, 0, offset+2)
			for i := 0; i < offset; i++ {
				expected = append(expected, 'x')
			}
			expected = append(expected, '\n', 'y')

			result, consumed := parseShortStringAVX2(input)
			if string(result) != string(expected) {
				t.Errorf("escape at offset %d failed:\n  got: %q (len=%d)\n  want: %q (len=%d)",
					offset, result, len(result), expected, len(expected))
			}
			if consumed != int64(len(input)) {
				t.Errorf("consumed = %d, want %d", consumed, len(input))
			}
		})
	}
}
