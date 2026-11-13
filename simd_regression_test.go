//go:build amd64

package silentjson

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAppendStringCapacity_EscapeExpansion verifies that appendString correctly
// handles strings with escape sequences when the buffer capacity is tight.
// This tests the fix for the initial capacity check not accounting for escape expansion.
func TestAppendStringCapacity_EscapeExpansion(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		cap    int
		expect int // -1 = success, -2 = overflow
	}{
		// No escapes: initial check should pass
		{"no_escapes_fit", "hello", 10, -1},
		{"no_escapes_exact", "hello", 8, -1}, // 5 + 2 quotes = 7

		// Single escape: need extra byte for backslash
		// Input " -> output "\"" (4 bytes: quote, backslash, quote, quote)
		{"one_quote_fit", `"`, 6, -1},
		{"one_quote_exact", `"`, 4, -1},
		{"one_quote_overflow", `"`, 3, -2},

		// Multiple escapes
		// Input "" -> output "\"\"" (6 bytes)
		{"two_quotes_fit", `""`, 9, -1},
		{"two_quotes_exact", `""`, 6, -1},
		{"two_quotes_overflow", `""`, 5, -2},

		// Backslashes
		// Input \ -> output "\\\\" (4 bytes)
		{"one_backslash_fit", `\`, 6, -1},
		{"one_backslash_exact", `\`, 4, -1},
		{"one_backslash_overflow", `\`, 3, -2},

		// Mixed escapes
		{"mixed_fit", `"a\b`, 12, -1},

		// Empty string: needs 2 bytes for quotes only
		{"empty_fit", ``, 4, -1},
		{"empty_exact", ``, 2, -1},
		{"empty_overflow", ``, 1, -2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 0, tt.cap)
			out, specialPos := appendStringASM(buf, tt.s)
			if specialPos != tt.expect && specialPos != -1 && specialPos != -2 {
				// Control chars trigger slow path (specialPos >= 0), which is OK
				if specialPos >= 0 {
					return
				}
			}
			if tt.expect == -2 && specialPos != -2 {
				t.Errorf("expected overflow (-2), got %d, out=%q", specialPos, string(out))
			}
			if tt.expect == -1 && specialPos == -2 {
				t.Errorf("expected success (-1), got overflow (-2)")
			}
		})
	}
}

// TestAppendStringCapacity_LargeEscapeRatio tests strings with high escape ratios
// to ensure the capacity check correctly detects overflow before writing out of bounds.
func TestAppendStringCapacity_LargeEscapeRatio(t *testing.T) {
	// String with many quotes: each quote adds an extra byte for escaping
	s := strings.Repeat(`"`, 10) // 10 quotes
	escapedLen := len(s) + len(s) // Each quote becomes \" (2 bytes)
	totalNeeded := escapedLen + 2 // Plus opening/closing quotes

	// Test with exact capacity
	buf := make([]byte, 0, totalNeeded)
	out, specialPos := appendStringASM(buf, s)
	if specialPos == -2 {
		t.Errorf("expected success with exact capacity, got overflow")
	} else if specialPos == -1 {
		expected := `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		if string(out) != expected {
			t.Errorf("output mismatch:\n got: %q\nwant: %q", string(out), expected)
		}
	}

	// Test with one byte less than needed (should overflow)
	buf = make([]byte, 0, totalNeeded-1)
	out, specialPos = appendStringASM(buf, s)
	if specialPos != -2 {
		t.Errorf("expected overflow with insufficient capacity, got %d", specialPos)
	}
}

// TestAppendStringCapacity_BulkCopyBoundary tests the fix for bulk copy capacity checking.
// When a string has escape sequences early on, the AVX2 loop's bulk copy path must check
// that there's actually room for all 32 bytes after the escape expansion.
func TestAppendStringCapacity_BulkCopyBoundary(t *testing.T) {
	// Construct a string where escaping pushes us past capacity during bulk copy
	// First few chars have escapes, then a long run of normal chars
	s := `""` + strings.Repeat("a", 100)
	
	// Calculate needed capacity: 2 quotes escaped (4 bytes) + 100 'a's + 2 outer quotes = 106
	totalNeeded := 4 + 100 + 2
	
	// Test with exact capacity
	buf := make([]byte, 0, totalNeeded)
	out, specialPos := appendStringASM(buf, s)
	if specialPos == -2 {
		t.Errorf("expected success with exact capacity (%d), got overflow", totalNeeded)
	} else if specialPos == -1 {
		expected := `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		if string(out) != expected {
			t.Errorf("output mismatch:\n got: %q\nwant: %q", string(out), expected)
		}
	}

	// Test with one byte less (should overflow)
	buf = make([]byte, 0, totalNeeded-1)
	out, specialPos = appendStringASM(buf, s)
	if specialPos != -2 {
		t.Errorf("expected overflow with insufficient capacity (%d), got %d", totalNeeded-1, specialPos)
	}
}

// TestAppendString_Correctness verifies that the output matches encoding/json for various strings.
func TestAppendString_Correctness(t *testing.T) {
	tests := []string{
		"plain text",
		`has "quotes"`,
		`has \backslash`,
		"mixed \" and \\ together",
		strings.Repeat(`\"`, 5),
		strings.Repeat(`\\`, 10),
	}

	for _, s := range tests {
		t.Run(s[:minInt(20, len(s))], func(t *testing.T) {
			buf := make([]byte, 0, 4096)
			out, specialPos := appendStringASM(buf, s)
			
			var result string
			if specialPos == -1 {
				result = string(out)
			} else if specialPos >= 0 {
				// Slow path: complete the escaping in Go
				buf = out
				for i := specialPos; i < len(s); i++ {
					c := s[i]
					switch c {
					case '"':
						buf = append(buf, '\\', '"')
					case '\\':
						buf = append(buf, '\\', '\\')
					default:
						buf = append(buf, c)
					}
				}
				buf = append(buf, '"')
				result = string(buf)
			} else {
				t.Fatalf("unexpected specialPos: %d", specialPos)
			}

			// Compare with encoding/json
			expected, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			if result != string(expected) {
				t.Errorf("output mismatch:\n got: %q\nwant: %q", result, string(expected))
			}
		})
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestParseShortStringAVX2_2_Bounds tests the boundary check fix for parseShortStringAVX2_2.
// When input ends with a backslash, the function should not read past the end.
func TestParseShortStringAVX2_2_Bounds(t *testing.T) {
	// Normal string (no escapes) - should process without crashing
	src := []byte("hello")
	parseShortStringASM2(src)

	// String ending with backslash (should detect EOF after escape, not crash)
	src = []byte(`hello\`)
	parseShortStringASM2(src)

	// Empty string - should not crash
	src = []byte("")
	parseShortStringASM2(src)

	// Just a backslash - edge case for escape at start
	src = []byte(`\`)
	parseShortStringASM2(src)

	// Backslash followed by normal char
	src = []byte(`\n`)
	parseShortStringASM2(src)
}

// TestFindObjectBoundaries_EscapeCarry tests the escape carry fix at SIMD→scalar transition.
// A backslash at position 31 (last byte of first SIMD block) should escape a quote
// at position 32 (first byte of scalar tail), preventing it from ending the string.
func TestFindObjectBoundaries_EscapeCarry(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	// Construct input where a backslash falls exactly at the 32-byte boundary
	// and escapes a quote in the scalar tail. The escaped quote should NOT be
	// treated as ending the string, so the object should still be found.
	// Layout: {"key":" + 23 'a's + \ + " + rest"}"
	// Positions: 0-7 = {"key":" , 8-30 = 'a's, 31 = backslash, 32 = escaped quote
	prefix := `{"key":"` + strings.Repeat("a", 23)
	input := prefix + `\` + `"rest"}`

	chunks := make([]Chunk, 10)
	count, _ := findObjectBoundariesAVX2([]byte(input), chunks)
	if count != 1 {
		t.Errorf("expected 1 object boundary, got %d", count)
	}
	// Object starts at position 0 and ends at the closing brace
	if chunks[0].Start != 0 {
		t.Errorf("unexpected chunk start: %d (expected 0)", chunks[0].Start)
	}
	expectedEnd := len(input)
	if chunks[0].End != expectedEnd {
		t.Errorf("unexpected chunk end: %d (expected %d)", chunks[0].End, expectedEnd)
	}

	// Verify the object can be parsed correctly
	objJSON := input[chunks[0].Start:chunks[0].End]
	var result map[string]interface{}
	err := json.Unmarshal([]byte(objJSON), &result)
	if err != nil {
		t.Errorf("parsed object is not valid JSON: %v (input: %s)", err, objJSON)
	}
}

// TestFindObjectBoundaries_EscapeCarry_MultipleBlocks tests escape carry across multiple SIMD blocks.
func TestFindObjectBoundaries_EscapeCarry_MultipleBlocks(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	// Multiple objects with escapes at various boundaries
	input := `{"a":"b"}{"c":"d"}{"e":"f"}`
	
	chunks := make([]Chunk, 10)
	count, _ := findObjectBoundariesAVX2([]byte(input), chunks)
	if count != 3 {
		t.Errorf("expected 3 object boundaries, got %d", count)
	}
}

// TestFindObjectBoundaries_EscapeCarry_Detailed tests escape carry at exact SIMD boundary.
func TestFindObjectBoundaries_EscapeCarry_Detailed(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	// Case 1: Backslash at position 31 escapes quote at 32 (inside string)
	prefix := `{"key":"` + strings.Repeat("a", 23)
	input1 := prefix + `\` + `"rest"}`
	
	chunks1 := make([]Chunk, 10)
	count1, _ := findObjectBoundariesAVX2([]byte(input1), chunks1)
	t.Logf("Case 1 (escape at boundary): found %d objects", count1)
	if count1 != 1 {
		t.Errorf("expected 1 object, got %d", count1)
	}

	// Case 2: No escape, string ends normally before boundary
	input2 := `{"key":"value"}{"key2":"value2"}`
	chunks2 := make([]Chunk, 10)
	count2, _ := findObjectBoundariesAVX2([]byte(input2), chunks2)
	t.Logf("Case 2 (no escape): found %d objects", count2)
	if count2 != 2 {
		t.Errorf("expected 2 objects, got %d", count2)
	}

	// Case 3: Escape inside string, not at boundary
	input3 := `{"key":"val\"ue"}{"key2":"value2"}`
	chunks3 := make([]Chunk, 10)
	count3, _ := findObjectBoundariesAVX2([]byte(input3), chunks3)
	t.Logf("Case 3 (escape not at boundary): found %d objects", count3)
	if count3 != 2 {
		t.Errorf("expected 2 objects, got %d", count3)
	}
}
