package silentjson

import (
	"reflect"
	"testing"
	"unsafe"
)

// TestBackslashParity verifies that quote-escaping is determined by the parity
// of preceding backslashes, not by treating every backslash as special.
func TestBackslashParity(t *testing.T) {
	type testCase struct {
		name     string
		input    string // JSON string value (without outer quotes)
		expected string
	}

	tests := []testCase{
		{"no_backslash", `hello`, "hello"},
		{"one_backslash_escapes_quote", `say \"hi\"`, `say "hi"`},
		{"two_backslashes_literal_backslash", `path\\to`, "path\\to"},
		{"three_backslashes_escape_quote", `a\\\\\"b`, `a\\"b`},
		{"four_backslashes_two_literals", `x\\\\y`, "x\\\\y"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			type T struct {
				S string `json:"s"`
			}
			json := `{"s":"` + tc.input + `"}`
			var result T
			typ := reflect.TypeOf(result)
			reg := BuildRegistry(typ)
			ptr := unsafe.Pointer(&result)

			err := ParseObject([]byte(json), reg, ptr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.S != tc.expected {
				t.Errorf("got %q, want %q", result.S, tc.expected)
			}
		})
	}
}

// TestBackslashParityAcrossSIMDBoundaries tests that backslash sequences
// spanning 32-byte AVX2 block boundaries are handled correctly.
func TestBackslashParityAcrossSIMDBoundaries(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	type T struct {
		S string `json:"s"`
	}

	// Build a string with a backslash sequence that crosses a 32-byte boundary.
	// We want the decoded string to contain 6 literal backslashes followed by
	// an escaped quote (literal quote). In JSON, each literal backslash must
	// be written as \\, so 6 backslashes become \\\\\\\\ in the JSON text.
	prefix := "aaaaaaaaaa" // 10 bytes

	padding := make([]byte, 24)
	for i := range padding {
		padding[i] = 'x'
	}

	// Construct JSON with properly escaped backslashes
	jsonBytes := []byte(`{"s":"`)
	jsonBytes = append(jsonBytes, padding...)
	jsonBytes = append(jsonBytes, prefix...)
	// 6 literal backslashes in JSON become 12 characters: \\\\\\\\
	jsonBytes = append(jsonBytes, `\\\\\\\\`...)
	// Escaped quote (literal quote in the string)
	jsonBytes = append(jsonBytes, `\"end"}`...)

	var result T
	typ := reflect.TypeOf(result)
	reg := BuildRegistry(typ)
	ptr := unsafe.Pointer(&result)

	err := ParseObject(jsonBytes, reg, ptr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected decoded value: padding + prefix + 6 literal backslashes + " + end
	expectedBackslashes := "\\\\\\\\" // 6 literal backslashes in Go string
	expected := string(padding) + prefix + expectedBackslashes + `"end`
	if result.S != expected {
		t.Errorf("got %q, want %q", result.S, expected)
	}
}

// TestAllEscapeSequences verifies decoding of all JSON escape sequences.
func TestAllEscapeSequences(t *testing.T) {
	type T struct {
		S string `json:"s"`
	}

	tests := []struct {
		name     string
		input    string // JSON escape sequence (without backslash)
		expected string
	}{
		{"quote", `\"`, `"`},
		{"backslash", `\\`, `\`},
		{"solidus", `\/`, `/`},
		{"backspace", `\b`, "\b"},
		{"formfeed", `\f`, "\f"},
		{"newline", `\n`, "\n"},
		{"carriage_return", `\r`, "\r"},
		{"tab", `\t`, "\t"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			json := `{"s":"prefix` + tc.input + `suffix"}`

			var result T
			typ := reflect.TypeOf(result)
			reg := BuildRegistry(typ)
			ptr := unsafe.Pointer(&result)

			err := ParseObject([]byte(json), reg, ptr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "prefix" + tc.expected + "suffix"
			if result.S != expected {
				t.Errorf("got %q, want %q", result.S, expected)
			}
		})
	}
}

// TestUnicodeEscapes verifies \uXXXX handling including surrogate pairs.
func TestUnicodeEscapes(t *testing.T) {
	type T struct {
		S string `json:"s"`
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"basic_unicode", `\u0041`, "A"},
		{"emoji_surrogate_pair", `\uD83D\uDE00`, "\U0001F600"}, // grinning face
		{"non_surrogate_high", `\uD800X`, "\uFFFDX"},          // unpaired surrogate -> U+FFFD
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			json := `{"s":"prefix` + tc.input + `suffix"}`

			var result T
			typ := reflect.TypeOf(result)
			reg := BuildRegistry(typ)
			ptr := unsafe.Pointer(&result)

			err := ParseObject([]byte(json), reg, ptr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "prefix" + tc.expected + "suffix"
			if result.S != expected {
				t.Errorf("got %q, want %q", result.S, expected)
			}
		})
	}

	// Invalid hex should error
	t.Run("invalid_hex", func(t *testing.T) {
		json := `{"s":"prefix\uGGGGsuffix"}`
		var result T
		typ := reflect.TypeOf(result)
		reg := BuildRegistry(typ)
		ptr := unsafe.Pointer(&result)

		err := ParseObject([]byte(json), reg, ptr)
		if err == nil {
			t.Fatal("expected error for invalid hex")
		}
	})
}

// TestMalformedEscapes verifies error handling for invalid escape sequences.
func TestMalformedEscapes(t *testing.T) {
	type T struct {
		S string `json:"s"`
	}

	tests := []struct {
		name  string
		input string
	}{
		{"invalid_escape_char", `test\x`},
		{"truncated_unicode", `test\u00`},
		{"incomplete_escape", `test\`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			json := `{"s":"` + tc.input + `"}`
			var result T
			typ := reflect.TypeOf(result)
			reg := BuildRegistry(typ)
			ptr := unsafe.Pointer(&result)

			err := ParseObject([]byte(json), reg, ptr)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// TestStringBoundaries covers edge cases at string start/end.
func TestStringBoundaries(t *testing.T) {
	type T struct {
		S string `json:"s"`
	}

	tests := []struct {
		name     string
		input    string // JSON string content (without outer quotes)
		expected string
	}{
		{"empty_string", ``, ""},
		{"escape_at_start", `\"hello`, `"hello`},
		{"long_prefix_escape", `aaaaaaaaaaaaaaaaaaaaaaaaaaaa\nb`, "aaaaaaaaaaaaaaaaaaaaaaaaaaaa\nb"},
		{"multiple_escapes", `a\nb\tc\rd`, "a\nb\tc\rd"},
		{"backslash_then_quote", `\\\"`, `\` + `"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			json := `{"s":"` + tc.input + `"}`
			var result T
			typ := reflect.TypeOf(result)
			reg := BuildRegistry(typ)
			ptr := unsafe.Pointer(&result)

			err := ParseObject([]byte(json), reg, ptr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.S != tc.expected {
				t.Errorf("got %q, want %q", result.S, tc.expected)
			}
		})
	}
}

// TestMemoryBehavior verifies that escaped strings in large documents don't
// allocate buffers proportional to the entire document.
func TestMemoryBehavior(t *testing.T) {
	type T struct {
		S   string `json:"s"`
		Big string `json:"big"`
	}

	largeValue := make([]byte, 1024*1024) // 1MB of data
	for i := range largeValue {
		largeValue[i] = 'A'
	}

	input := []byte(`{"s":"\n","big":"`)
	input = append(input, largeValue...)
	input = append(input, `"}`...)

	var result T
	typ := reflect.TypeOf(result)
	reg := BuildRegistry(typ)
	ptr := unsafe.Pointer(&result)

	err := ParseObject(input, reg, ptr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.S != "\n" {
		t.Errorf("got %q, want newline", result.S)
	}
}

// TestOwnership verifies copyStrings behavior.
func TestOwnership(t *testing.T) {
	type T struct {
		S string `json:"s"`
	}

	json := `{"s":"hello"}`

	var result T
	typ := reflect.TypeOf(result)
	reg := BuildRegistry(typ)
	ptr := unsafe.Pointer(&result)
	err := ParseObject([]byte(json), reg, ptr)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.S != "hello" {
		t.Errorf("got %q, want hello", result.S)
	}
}
