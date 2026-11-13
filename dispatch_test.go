//go:build amd64

package silentjson

import (
	"testing"
)

// TestUseArraySplitterAVX2_PureFunction verifies the dispatch decision logic
// for all four CPU feature combinations. This tests only the pure function,
// never executing assembly with faked feature flags.
func TestUseArraySplitterAVX2_PureFunction(t *testing.T) {
	testCases := []struct {
		name     string
		avx2     bool
		pclmul   bool
		expected bool
	}{
		{"AVX2+PCLMUL", true, true, true},
		{"AVX2 only", true, false, false},
		{"PCLMUL only", false, true, false},
		{"neither", false, false, false},
	}

	for _, tc := range testCases {
		got := useArraySplitterAVX2(tc.avx2, tc.pclmul)
		if got != tc.expected {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.expected, got)
		}
	}
}

// TestCPUFeatureDispatch verifies that the dispatch logic selects the correct
// implementation based on actual CPU features. Uses real hardware flags only.
func TestCPUFeatureDispatch(t *testing.T) {
	t.Logf("hasAVX2=%v, hasPCLMULQDQ=%v", hasAVX2, hasPCLMULQDQ)

	// Test findQuoteAsm dispatch with actual CPU features
	input := []byte(`hello"world`)
	expectedQuoteIdx := 5

	quoteIdx := findQuoteAsm(input)
	if quoteIdx != expectedQuoteIdx {
		t.Errorf("findQuoteAsm failed, got %d, expected %d", quoteIdx, expectedQuoteIdx)
	}

	// Test parseShortStringASM (no escapes, should return full string up to quote)
	result, consumed := parseShortStringASM([]byte(`hello"`))
	if string(result) != "hello" || consumed != 6 {
		t.Errorf("parseShortStringASM failed: %q (consumed=%d)", string(result), consumed)
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
// the correct implementation based on actual CPU features and produces consistent
// results. Uses real hardware flags only - never fakes them.
func TestArraySplitterDispatch(t *testing.T) {
	t.Logf("hasAVX2=%v, hasPCLMULQDQ=%v -> use AVX2: %v", hasAVX2, hasPCLMULQDQ, useArraySplitterAVX2(hasAVX2, hasPCLMULQDQ))

	// Test input: array inner content (after '[' and before ']')
	input := []byte(`{"a":1},{"b":"hello"},{"c":true},{"d":null}`)
	chunks := make([]Chunk, 10)

	count, depth := findArrayElementsEarlyExitASM(input, chunks)
	t.Logf("count=%d, depth=%d", count, depth)
	for i := 0; i < count; i++ {
		t.Logf("  chunk %d: start=%d end=%d content=%q", i, chunks[i].Start, chunks[i].End, string(input[chunks[i].Start:chunks[i].End]))
	}

	// Expect 3 elements (last not emitted without trailing bracket)
	if count != 3 {
		t.Errorf("expected 3 elements (last not emitted without trailing bracket), got %d", count)
	}

	// Verify chunk boundaries are correct for the first 3 elements
	expectedBoundaries := [][2]int{
		{0, 7},   // {"a":1}
		{8, 21},  // {"b":"hello"}
		{22, 32}, // {"c":true}
	}

	for i := 0; i < count && i < len(expectedBoundaries); i++ {
		if int(chunks[i].Start) != expectedBoundaries[i][0] || int(chunks[i].End) != expectedBoundaries[i][1] {
			t.Errorf("chunk %d has wrong boundaries (got %d-%d, expected %d-%d)",
				i, chunks[i].Start, chunks[i].End,
				expectedBoundaries[i][0], expectedBoundaries[i][1])
		}
	}

	// If AVX2 path was used, verify it matches scalar output
	if useArraySplitterAVX2(hasAVX2, hasPCLMULQDQ) {
		scalarChunks := make([]Chunk, 10)
		scalarCount, _ := findArrayElementsEarlyExitScalar(input, scalarChunks)

		if scalarCount != count {
			t.Errorf("AVX2 vs scalar: count mismatch (AVX2=%d, scalar=%d)", count, scalarCount)
		} else {
			for i := 0; i < count; i++ {
				if chunks[i] != scalarChunks[i] {
					t.Errorf("AVX2 vs scalar: chunk %d mismatch (AVX2=%+v, scalar=%+v)", i, chunks[i], scalarChunks[i])
				}
			}
		}
	}
}
