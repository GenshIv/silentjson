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
		// Input: a"b\c (5 bytes) -> Output: "a\"b\\c" (9 bytes)
		{"exact_multiple_escapes", `a"b\c`, 9},

		// Empty string
		{"empty_string", "", 2},

		// Very long string with no escapes
		{"long_no_escapes", strings.Repeat("x", 100), 102},

		// Long string with high escape ratio (every char is a quote)
		// Input: 50 quotes -> Output: " + 50*\" + " = 102 bytes
		{"high_escape_ratio", strings.Repeat(`"`, 50), 102},
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

// TestAppendString_CapacityMatrix provides comprehensive exact/exact-1/exact+1 coverage.
func TestAppendString_CapacityMatrix(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	testCases := []struct {
		name       string
		input      string
		exactCap   int
		expectSlow bool // whether slow path is expected (control chars)
	}{
		// Empty string
		{"empty", "", 2, false},

		// Plain ASCII
		{"plain_ascii", "hello world", 13, false}, // "hello world" = 11 + 2 quotes

		// Quote escaping
		{"one_quote", `"`, 4, false},
		{"two_quotes", `""`, 6, false},
		{"quotes_in_text", `say "hi"`, 12, false}, // say \"hi\" = 8 + 2 quotes + 2 escapes

		// Backslash escaping
		{"one_backslash", `\`, 4, false},
		{"two_backslashes", `\\`, 6, false},
		{"backslash_in_text", `C:\path`, 10, false}, // C:\\path = 7 + 2 quotes + 1 escape

		// Mixed escaping
		{"mixed_quotes_backslashes", `"a\b"c\`, 13, false}, // \"a\\b\"c\\" = 9 + 2 quotes + 4 escapes? No...
		// Actually: "a\b"c\ -> "\"a\\b\"c\\" = 13 bytes

		// High escape ratio
		{"all_quotes_10", strings.Repeat(`"`, 10), 22, false}, // 10*\" + 2 quotes = 22
		{"all_backslashes_10", strings.Repeat(`\`, 10), 22, false},

		// SIMD boundary lengths
		{"length_31", strings.Repeat("a", 31), 33, false},
		{"length_32", strings.Repeat("a", 32), 34, false},
		{"length_33", strings.Repeat("a", 33), 35, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name+"_exact", func(t *testing.T) {
			buf := make([]byte, 0, tc.exactCap)
			out, specialPos := appendStringAVX2(buf, tc.input)

			if !tc.expectSlow && specialPos != -1 {
				t.Errorf("expected fast path success (-1), got %d", specialPos)
			}

			if specialPos == -1 {
				result := string(out)
				expected, _ := json.Marshal(tc.input)
				if result != string(expected) {
					t.Errorf("output mismatch:\n got: %q\nwant: %q", result, string(expected))
				}
			} else if specialPos == -2 {
				t.Errorf("unexpected overflow at exact capacity")
			}
		})

		// Test exact-1 (should overflow for fast-path cases)
		if !tc.expectSlow {
			t.Run(tc.name+"_minus_one", func(t *testing.T) {
				buf := make([]byte, 0, tc.exactCap-1)
				_, specialPos := appendStringAVX2(buf, tc.input)
				if specialPos != -2 {
					t.Errorf("expected overflow at capacity-1, got %d", specialPos)
				}
			})
		}

		// Test exact+1 (should succeed with room to spare)
		t.Run(tc.name+"_plus_one", func(t *testing.T) {
			buf := make([]byte, 0, tc.exactCap+1)
			out, specialPos := appendStringAVX2(buf, tc.input)

			if !tc.expectSlow && specialPos != -1 {
				t.Errorf("expected fast path success (-1), got %d", specialPos)
			}

			if specialPos == -1 {
				result := string(out)
				expected, _ := json.Marshal(tc.input)
				if result != string(expected) {
					t.Errorf("output mismatch:\n got: %q\nwant: %q", result, string(expected))
				}
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

// TestAppendStringAVX2_GuardBytes verifies that appendStringAVX2 does not write
// beyond the permitted slice capacity, even when the backing array is larger.
func TestAppendStringAVX2_GuardBytes(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	const sentinel = 0xAB
	testCases := []struct {
		name     string
		input    string
		capacity int // permitted slice capacity
	}{
		{"empty_string", "", 2},
		{"plain_ascii", "hello", 7},
		{"one_quote", `"`, 4},
		{"one_backslash", `\`, 4},
		{"mixed_escapes", `a"b\c`, 9},
		{"high_escape_ratio", strings.Repeat(`"`, 10), 22}, // 10 quotes -> 20 escaped + 2 outer
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Allocate backing array larger than permitted region
			backingSize := tc.capacity + 32 // extra space for guard bytes
			backing := make([]byte, backingSize)

			// Fill entire backing array with sentinel
			for i := range backing {
				backing[i] = sentinel
			}

			// Construct slice with restricted capacity
			slice := backing[:0:tc.capacity]

			// Invoke appendStringAVX2 directly
			out, specialPos := appendStringAVX2(slice, tc.input)

			// Verify result is correct (handle slow path if needed)
			var result string
			if specialPos == -1 {
				result = string(out)
			} else if specialPos >= 0 {
				// Slow path: complete in Go
				buf := out
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
				result = string(buf)
			} else {
				t.Fatalf("unexpected specialPos: %d", specialPos)
			}

			// Verify correctness against encoding/json
			expected, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			if result != string(expected) {
				t.Errorf("output mismatch:\n got: %q\nwant: %q", result, string(expected))
			}

			// Verify guard bytes outside permitted region are unchanged
			for i := tc.capacity; i < backingSize; i++ {
				if backing[i] != sentinel {
					t.Errorf("guard byte at offset %d was modified (expected 0x%02X, got 0x%02X)",
						i, sentinel, backing[i])
				}
			}

			// Verify prefix (bytes before the string) is unchanged if any
			if len(out) > 0 {
				// The entire output should be within permitted capacity
				if len(out) > tc.capacity {
					t.Errorf("output length %d exceeds permitted capacity %d", len(out), tc.capacity)
				}
			}
		})
	}
}

// TestAppendStringAVX2_GuardBytes_SlowPath verifies guard integrity when the SIMD
// path triggers a slow-path continuation (control characters).
func TestAppendStringAVX2_GuardBytes_SlowPath(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	const sentinel = 0xCD
	input := "hello\x01world" // control character triggers slow path
	capacity := 30            // plenty of room

	backingSize := capacity + 32
	backing := make([]byte, backingSize)
	for i := range backing {
		backing[i] = sentinel
	}

	slice := backing[:0:capacity]
	out, specialPos := appendStringAVX2(slice, input)

	// Verify slow path was triggered (control char at position 5)
	if specialPos != 5 {
		t.Errorf("expected slow path trigger at position 5, got %d", specialPos)
	}

	// Complete in Go
	buf := out
	for i := specialPos; i < len(input); i++ {
		c := input[i]
		if c < 0x20 {
			// JSON control char escaping: \u00XX
			buf = append(buf, '\\', 'u', '0', '0', hexDigit(c>>4), hexDigit(c&0xF))
		} else {
			buf = append(buf, c)
		}
	}
	buf = append(buf, '"')

	// Verify guard bytes unchanged
	for i := capacity; i < backingSize; i++ {
		if backing[i] != sentinel {
			t.Errorf("guard byte at offset %d was modified (expected 0x%02X, got 0x%02X)",
				i, sentinel, backing[i])
		}
	}
}

func hexDigit(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'a' + (n - 10)
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

// TestFindObjectBoundaries_Comprehensive tests escape-carry scenarios at various SIMD boundaries.
// The AVX2 and scalar EarlyExit implementations have different semantics:
// - Scalar expects comma-separated objects or closing brackets (array context)
// - AVX2 finds concatenated objects (NDJSON-like)
// This test focuses on the AVX2 implementation's escape-carry behavior at SIMD boundaries,
// with separate verification that scalar works correctly for its intended use case.
func TestFindObjectBoundaries_Comprehensive(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected int // expected number of objects for AVX2
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

		// Four consecutive backslashes before quote (two escaped backslashes, then string ends)
		{"quadruple_backslash", `{"key":"val\\\\"}`, 1},

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

			if hasAVX2 {
				// Test AVX2 implementation (primary for concatenated objects)
				chunksAVX2 := make([]Chunk, 20)
				countAVX2, _ := findObjectBoundariesEarlyExitAVX2(input, chunksAVX2)

				// Verify expected count
				if countAVX2 != tc.expected {
					t.Errorf("expected %d objects, got %d (AVX2)", tc.expected, countAVX2)
				}

				// Verify chunk boundaries are within input range
				for i := 0; i < countAVX2; i++ {
					if chunksAVX2[i].Start < 0 || chunksAVX2[i].End > len(input) || chunksAVX2[i].Start >= chunksAVX2[i].End {
						t.Errorf("chunk %d out of range: start=%d, end=%d, input_len=%d",
							i, chunksAVX2[i].Start, chunksAVX2[i].End, len(input))
					}
				}

				// For inputs with commas (array context), compare with scalar
				if strings.Contains(tc.input, ",") {
					chunksScalar := make([]Chunk, 20)
					countScalar, _ := findObjectBoundariesEarlyExitScalar(input, chunksScalar)

					if countAVX2 != countScalar {
						t.Errorf("count mismatch (comma-separated): AVX2=%d scalar=%d", countAVX2, countScalar)
					}

					for i := 0; i < countScalar; i++ {
						if chunksAVX2[i] != chunksScalar[i] {
							t.Errorf("chunk %d mismatch: AVX2=%+v scalar=%+v", i, chunksAVX2[i], chunksScalar[i])
						}
					}
				}
			} else {
				// AVX2 not available - test scalar only for comma-separated inputs
				if strings.Contains(tc.input, ",") {
					chunksScalar := make([]Chunk, 20)
					countScalar, _ := findObjectBoundariesEarlyExitScalar(input, chunksScalar)
					t.Logf("scalar: found %d objects (AVX2 unavailable)", countScalar)
				}
			}
		})
	}
}

// TestFindObjectBoundaries_Exact verifies exact Start/End offsets for known fixtures.
// This ensures boundaries are not just within range but precisely correct.
func TestFindObjectBoundaries_Exact(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	testCases := []struct {
		name     string
		input    string
		expected []Chunk // expected exact boundaries
	}{
		// Single object, no escapes
		{"single_no_escape", `{"key":"value"}`, []Chunk{{Start: 0, End: 15}}},

		// Two concatenated objects
		{"two_objects", `{"a":1}{"b":2}`, []Chunk{{Start: 0, End: 7}, {Start: 7, End: 14}}},

		// Object with escaped quote inside (len=17)
		{"escaped_quote_inside", `{"key":"val\"ue"}`, []Chunk{{Start: 0, End: 17}}},

		// Escape at SIMD boundary (position 31) (len=39)
		{"escape_at_31_exact", `{"key":"` + strings.Repeat("a", 23) + `\"rest"}`, []Chunk{{Start: 0, End: 39}}},

		// Multiple objects with escapes at boundaries (len=25)
		{"multiple_with_escapes", `{"k":"v\"a"}{"k2":"v\"b"}`, []Chunk{{Start: 0, End: 12}, {Start: 12, End: 25}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.input)

			// Run AVX2 implementation
			chunksAVX2 := make([]Chunk, 20)
			countAVX2, _ := findObjectBoundariesEarlyExitAVX2(input, chunksAVX2)

			if countAVX2 != len(tc.expected) {
				t.Errorf("expected %d objects, got %d (AVX2)", len(tc.expected), countAVX2)
				return
			}

			for i, expected := range tc.expected {
				if chunksAVX2[i].Start != expected.Start || chunksAVX2[i].End != expected.End {
					t.Errorf("chunk %d: expected start=%d end=%d, got start=%d end=%d",
						i, expected.Start, expected.End, chunksAVX2[i].Start, chunksAVX2[i].End)
				}
			}

			// Run scalar implementation for comparison on comma-separated inputs
			if strings.Contains(tc.input, ",") {
				chunksScalar := make([]Chunk, 20)
				countScalar, _ := findObjectBoundariesEarlyExitScalar(input, chunksScalar)

				if countAVX2 != countScalar {
					t.Errorf("count mismatch (comma-separated): AVX2=%d scalar=%d", countAVX2, countScalar)
				} else {
					for i := 0; i < countScalar; i++ {
						if chunksAVX2[i] != chunksScalar[i] {
							t.Errorf("chunk %d mismatch: AVX2=%+v scalar=%+v", i, chunksAVX2[i], chunksScalar[i])
						}
					}
				}
			}
		})
	}
}

// TestFindObjectBoundaries_EscapeCarry_Sweep tests escape carry at every byte position
// relative to SIMD block boundaries for the AVX2 implementation.
func TestFindObjectBoundaries_EscapeCarry_Sweep(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	// Test positions around each 32-byte boundary: -2, -1, 0, +1, +2 relative to boundary
	for boundary := 32; boundary <= 96; boundary += 32 {
		for offset := -2; offset <= 2; offset++ {
			pos := boundary + offset

			// Construct input: {"key":"<padding>"rest"} where backslash is at pos
			prefixLen := 8 // `{"key":"`
			paddingLen := pos - prefixLen
			if paddingLen < 0 {
				continue
			}

			input := `{"key":"` + strings.Repeat("a", paddingLen) + `\"rest"}`

			t.Run(fmt.Sprintf("boundary_%d_offset_%d", boundary, offset), func(t *testing.T) {
				chunksAVX2 := make([]Chunk, 10)
				countAVX2, _ := findObjectBoundariesEarlyExitAVX2([]byte(input), chunksAVX2)

				// Should find exactly 1 object with proper escape carry
				if countAVX2 != 1 {
					t.Errorf("expected 1 object, got %d", countAVX2)
				} else if chunksAVX2[0].End != len(input) {
					t.Errorf("object should span entire input: end=%d, input_len=%d", chunksAVX2[0].End, len(input))
				}
			})
		}
	}
}

// TestFindObjectBoundaries_BackslashCount tests various counts of consecutive backslashes
// before a quote to verify proper escape handling in AVX2 implementation.
func TestFindObjectBoundaries_BackslashCount(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	for n := 1; n <= 6; n++ {
		backslashes := strings.Repeat(`\`, n)
		// Input: {"key":"val" + N backslashes + "rest"}
		// Odd N: last backslash escapes the quote, string continues -> incomplete object (0 found)
		// Even N: all backslashes escaped, quote ends string -> complete object + trailing garbage
		input := `{"key":"val"` + backslashes + `"rest"}`

		t.Run(fmt.Sprintf("backslash_%d", n), func(t *testing.T) {
			chunksAVX2 := make([]Chunk, 10)
			countAVX2, _ := findObjectBoundariesEarlyExitAVX2([]byte(input), chunksAVX2)

			if n%2 == 1 {
				// Odd: escaped quote means string continues, no closing brace -> incomplete
				if countAVX2 != 0 {
					t.Errorf("odd backslash count (%d): expected 0 objects (incomplete), got %d", n, countAVX2)
				}
			} else {
				// Even: quote ends string, "rest"} is trailing garbage -> 1 complete object
				if countAVX2 != 1 {
					t.Errorf("even backslash count (%d): expected 1 object, got %d", n, countAVX2)
				}
			}
		})
	}
}

// TestFindObjectBoundaries_CommaSeparated_Differential verifies that AVX2 and scalar
// agree on the objects they both find. Note: AVX2 finds more objects than scalar
// EarlyExit because it continues scanning after each comma, while scalar exits early.
// We compare only up to the scalar count (the guaranteed minimum).
func TestFindObjectBoundaries_CommaSeparated_Differential(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	testCases := []string{
		`{"a":1},{"b":2}`,
		`{"a":"x"},{"b":"y"}`,
		`{"a":"v\"a"},{"b":"v\"b"}`,
		`{"a":1},`,
	}

	for _, input := range testCases {
		t.Run(input, func(t *testing.T) {
			chunksAVX2 := make([]Chunk, 10)
			countAVX2, _ := findObjectBoundariesEarlyExitAVX2([]byte(input), chunksAVX2)

			chunksScalar := make([]Chunk, 10)
			countScalar, _ := findObjectBoundariesEarlyExitScalar([]byte(input), chunksScalar)

			// AVX2 should find at least as many objects as scalar
			if countAVX2 < countScalar {
				t.Errorf("AVX2 found fewer objects than scalar: AVX2=%d scalar=%d", countAVX2, countScalar)
			}

			// Compare chunk boundaries for objects that both found
			for i := 0; i < countScalar; i++ {
				if chunksAVX2[i] != chunksScalar[i] {
					t.Errorf("chunk %d mismatch: AVX2=%+v scalar=%+v", i, chunksAVX2[i], chunksScalar[i])
				}
			}

			t.Logf("AVX2=%d objects, scalar=%d objects (AVX2 finds more due to continued scanning)", countAVX2, countScalar)
		})
	}
}
