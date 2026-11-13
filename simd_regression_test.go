//go:build amd64

package silentjson

import (
	"encoding/json"
	"fmt"
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

// TestAppendString_CapacityBoundaries tests exact capacity boundaries.
func TestAppendString_CapacityBoundaries(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	tests := []struct {
		name     string
		input    string
		capacity int
	}{
		// Exact capacity needed for plain string (no escapes)
		{"exact_plain", "hello", 7}, // "hello" = 5 + 2 quotes
        
		// One byte short of exact
		{"one_short", "hello", 6},
		
		// Exact capacity with one escaped quote
		{"exact_one_quote", `he"llo`, 9}, // he\"llo = 7 + 2 quotes
		
		// Exact capacity with one escaped backslash
		{"exact_one_backslash", "he\\llo", 9}, // he\\llo = 7 + 2 quotes
		
		// Exact capacity with multiple escapes
		{"exact_multiple_escapes", `a"b\c`, 11}, // a\"b\\c = 7 + 2 quotes
		
		// Empty string
		{"empty_string", "", 2},
		
		// Very long string with no escapes
		{"long_no_escapes", strings.Repeat("x", 100), 102},
		
		// Long string with high escape ratio (every other char is a quote)
		{"high_escape_ratio", strings.Repeat(`"`, 50), 152}, // 50 quotes -> 100 escaped bytes + 2 quotes
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, 0, tc.capacity)
			out, specialPos := appendStringASM(buf, tc.input)
			
			if specialPos == -1 {
				// Fast path succeeded - verify output is complete and correct
				result := string(out)
				expected, err := json.Marshal(tc.input)
				if err != nil {
					t.Fatalf("json.Marshal failed: %v", err)
				}
				if result != string(expected) {
					t.Errorf("fast path output mismatch:\n got: %q\nwant: %q", result, string(expected))
				}
			} else if specialPos >= 0 {
				// Slow path - verify the position is reasonable and complete manually
				if specialPos > len(tc.input) {
					t.Errorf("specialPos (%d) exceeds input length (%d)", specialPos, len(tc.input))
				}
				
				// Complete the escaping in Go and verify correctness
				buf = out
				for i := specialPos; i < len(tc.input); i++ {
					c := tc.input[i]
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
				
				result := string(buf)
				expected, err := json.Marshal(tc.input)
				if err != nil {
					t.Fatalf("json.Marshal failed: %v", err)
				}
				if result != string(expected) {
					t.Errorf("slow path output mismatch:\n got: %q\nwant: %q", result, string(expected))
				}
			} else if specialPos == -2 {
				// Overflow - expected for "one_short" case
				if tc.name != "one_short" {
					t.Errorf("unexpected overflow for %s", tc.name)
				}
			} else {
				t.Fatalf("unexpected specialPos: %d", specialPos)
			}
		})
	}
}

// TestAppendString_EscapeRatios tests various escape ratios to stress capacity estimation.
func TestAppendString_EscapeRatios(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	ratios := []struct {
		name   string
		generator func(n int) string
	}{
		{"no_escapes", func(n int) string { return strings.Repeat("a", n) }},
		{"all_quotes", func(n int) string { return strings.Repeat(`"`, n) }},
		{"all_backslashes", func(n int) string { return strings.Repeat(`\`, n) }},
		{"alternating", func(n int) string {
			s := strings.Builder{}
			for i := 0; i < n; i++ {
				if i%2 == 0 {
					s.WriteByte('"')
				} else {
					s.WriteByte('a')
				}
			}
			return s.String()
		}},
	}

	lengths := []int{16, 32, 64, 128}

	for _, ratio := range ratios {
		for _, length := range lengths {
			t.Run(fmt.Sprintf("%s_%d", ratio.name, length), func(t *testing.T) {
				input := ratio.generator(length)
				
				// Calculate worst-case capacity (all chars escaped)
				worstCase := 2*length + 2
				
				buf := make([]byte, 0, worstCase)
				out, specialPos := appendStringASM(buf, input)
				
				var result string
				if specialPos == -1 {
					result = string(out)
				} else {
					// Complete escaping in Go
					buf = out
					for i := specialPos; i < len(input); i++ {
						c := input[i]
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
				}
				
				expected, err := json.Marshal(input)
				if err != nil {
					t.Fatalf("json.Marshal failed: %v", err)
				}
				
				if result != string(expected) {
					t.Errorf("output mismatch for %s_%d:\n got: %q\nwant: %q", ratio.name, length, result, string(expected))
				}
			})
		}
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

// TestFindObjectBoundaries_Comprehensive compares AVX2 and scalar implementations
// across a comprehensive set of escape-carry scenarios at various SIMD boundaries.
func TestFindObjectBoundaries_Comprehensive(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected int // expected number of objects (for AVX2)
	}{
		// Escape at first SIMD boundary (position 31) - backslash escapes the quote
		{"escape_at_31", `{"key":"` + strings.Repeat("a", 23) + `\"rest"}`, 1},
		
		// Escape at second SIMD boundary (position 63)
		{"escape_at_63", `{"key":"` + strings.Repeat("a", 55) + `\"rest"}`, 1},
		
		// Escape at third SIMD boundary (position 95)
		{"escape_at_95", `{"key":"` + strings.Repeat("a", 87) + `\"rest"}`, 1},
		
		// Backslash not at boundary
		{"escape_not_at_boundary", `{"key":"val\"ue"}`, 1},
		
		// Two consecutive backslashes before quote (escaped backslash, then string ends)
		{"double_backslash", `{"key":"val\\"}`, 1},
		
		// Escaped backslash at end of string value
		{"backslash_at_end", `{"key":"val\\"}`, 1},
		
		// Four consecutive backslashes before quote (two escaped backslashes, then string ends)
		{"quadruple_backslash", `{"key":"val\\\\\\"}`, 1},
		
		// Multiple objects with escapes
		{"multiple_objects_escapes", `{"key":"v\"a"}{"key2":"v\"b"}`, 2},
		
		// Short input (less than 32 bytes)
		{"short_input", `{"key":"value"}`, 1},
		
		// Empty input
		{"empty_input", ``, 0},
		
		// No complete objects
		{"incomplete_object", `{"key":`, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.input)
			
			// Test AVX2 implementation (primary - handles concatenated objects)
			if hasAVX2 {
				chunksAVX2 := make([]Chunk, 20)
				countAVX2, _ := findObjectBoundariesAVX2(input, chunksAVX2)
				
				// Verify expected count for AVX2
				if countAVX2 != tc.expected {
					t.Errorf("AVX2: expected %d objects, got %d", tc.expected, countAVX2)
				}
				
				// Verify chunk boundaries are within input range
				for i := 0; i < countAVX2; i++ {
					if chunksAVX2[i].Start < 0 || chunksAVX2[i].End > len(input) || chunksAVX2[i].Start >= chunksAVX2[i].End {
						t.Errorf("AVX2 chunk %d out of range: start=%d, end=%d, input_len=%d",
							i, chunksAVX2[i].Start, chunksAVX2[i].End, len(input))
					}
				}
				
				// Test scalar implementation for comparison
				chunksScalar := make([]Chunk, 20)
				countScalar, _ := findObjectBoundariesScalar(input, chunksScalar)
				
				// Note: Scalar version has different semantics - it expects
				// comma-separated objects or closing brackets. It may not find
				// objects that AVX2 finds in concatenated input. We only compare
				// when both find objects.
				if countScalar > 0 && countAVX2 > 0 {
					minCount := countAVX2
					if countScalar < minCount {
						minCount = countScalar
					}
					for i := 0; i < minCount; i++ {
						if chunksAVX2[i].Start != chunksScalar[i].Start || chunksAVX2[i].End != chunksScalar[i].End {
							t.Errorf("AVX2 vs scalar: chunk %d mismatch (AVX2: %d-%d, scalar: %d-%d)",
								i, chunksAVX2[i].Start, chunksAVX2[i].End, chunksScalar[i].Start, chunksScalar[i].End)
						}
					}
				}
			} else {
				// AVX2 not available - test scalar only
				chunksScalar := make([]Chunk, 20)
				countScalar, _ := findObjectBoundariesScalar(input, chunksScalar)
				
				// Scalar may not find objects in concatenated input without
				// commas or brackets, so we don't enforce the expected count.
				t.Logf("scalar: found %d objects", countScalar)
			}
		})
	}
}
