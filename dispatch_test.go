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
