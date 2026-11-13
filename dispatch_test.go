//go:build amd64

package silentjson

import (
	"testing"
)

// TestCPUFeatureDispatch verifies that the dispatch logic selects the correct
// implementation based on CPU features. We override the feature flags to test
// different combinations and verify that all paths produce consistent results.
func TestCPUFeatureDispatch(t *testing.T) {
	// Save original values
	origAVX2 := hasAVX2
	origPCLMUL := hasPCLMULQDQ
	defer func() {
		hasAVX2 = origAVX2
		hasPCLMULQDQ = origPCLMUL
	}()

	// Test findQuoteAsm dispatch across all feature combinations
	input := []byte(`hello"world`)
	expectedQuoteIdx := 5

	testCases := []struct {
		name     string
		avx2     bool
		pclmul   bool
	}{
		{"AVX2+PCLMUL", true, true},
		{"AVX2 only", true, false},
		{"PCLMUL only", false, true},
		{"neither", false, false},
	}

	for _, tc := range testCases {
		hasAVX2 = tc.avx2
		hasPCLMULQDQ = tc.pclmul

		quoteIdx := findQuoteAsm(input)
		if quoteIdx != expectedQuoteIdx {
			t.Errorf("%s: findQuoteAsm failed, got %d, expected %d", tc.name, quoteIdx, expectedQuoteIdx)
		}

		// Test parseShortStringASM (no escapes, should return full string up to quote)
		result, consumed := parseShortStringASM([]byte(`hello"`))
		if string(result) != "hello" || consumed != 6 {
			t.Errorf("%s: parseShortStringASM failed: %q (consumed=%d)", tc.name, string(result), consumed)
		}
	}
}

// TestVPCLMULQDQDispatch documents the CPUID requirements for VPCLMULQDQ.
func TestVPCLMULQDQDispatch(t *testing.T) {
	t.Log("VPCLMULQDQ (VEX-encoded, XMM operands) requires:")
	t.Log("  - CPUID leaf 1 ECX bit 1 (PCLMULQDQ feature)")
	t.Log("  - AVX support for VEX prefix (implied by HasAVX2 check)")
	t.Log("")
	t.Log("There is NO separate CPUID bit for VPCLMULQDQ vs PCLMULQDQ.")
	t.Log("The AVX512 variant (EVEX-encoded, ZMM operands) uses leaf 7 EBX bit 30,")
	t.Log("but that's not used in this codebase.")
}

// TestArraySplitterDispatch verifies that findArrayElementsEarlyExitASM selects
// the correct implementation based on CPU features and produces consistent results.
func TestArraySplitterDispatch(t *testing.T) {
	origAVX2 := hasAVX2
	origPCLMUL := hasPCLMULQDQ
	defer func() {
		hasAVX2 = origAVX2
		hasPCLMULQDQ = origPCLMUL
	}()

	// Test input: array inner content (after '[' and before ']')
	// In production, this is what findArrayElementsEarlyExitASM receives.
	input := []byte(`{"a":1},{"b":"hello"},{"c":true},{"d":null}`)
	chunks := make([]Chunk, 10)

	// Note: The splitter only emits chunks when it sees a comma at depth 0
	// or when depth goes negative (closing bracket). For input without a
	// trailing bracket, the last element is not emitted. This matches
	// production usage where rawInner includes the closing ']'.

	testCases := []struct {
		name     string
		avx2     bool
		pclmul   bool
		expected string // expected implementation path
	}{
		{"AVX2+PCLMUL", true, true, "avx2"},
		{"AVX2 only", true, false, "scalar"},
		{"PCLMUL only", false, true, "scalar"},
		{"neither", false, false, "scalar"},
	}

	for _, tc := range testCases {
		hasAVX2 = tc.avx2
		hasPCLMULQDQ = tc.pclmul

		// Verify the dispatch path selection
		path := arraySplitterPath()
		if path != tc.expected {
			t.Errorf("%s: expected %s path, got %s", tc.name, tc.expected, path)
		}

		// Execute and verify results are consistent across all paths
		count, depth := findArrayElementsEarlyExitASM(input, chunks)
		t.Logf("%s: count=%d, depth=%d", tc.name, count, depth)
		for i := 0; i < count; i++ {
			t.Logf("  chunk %d: start=%d end=%d content=%q", i, chunks[i].Start, chunks[i].End, string(input[chunks[i].Start:chunks[i].End]))
		}

		if count != 3 {
			t.Errorf("%s: expected 3 elements (last not emitted without trailing bracket), got %d", tc.name, count)
		}

		// Verify chunk boundaries are correct for the first 3 elements
		expectedBoundaries := [][2]int{
			{0, 7},   // {"a":1}
			{8, 21},  // {"b":"hello"}
			{22, 32}, // {"c":true}
		}

		for i := 0; i < count && i < len(expectedBoundaries); i++ {
			if int(chunks[i].Start) != expectedBoundaries[i][0] || int(chunks[i].End) != expectedBoundaries[i][1] {
				t.Errorf("%s: chunk %d has wrong boundaries (got %d-%d, expected %d-%d)",
					tc.name, i, chunks[i].Start, chunks[i].End,
					expectedBoundaries[i][0], expectedBoundaries[i][1])
			}
		}
	}
}
