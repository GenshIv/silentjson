package silentjson

import (
	"testing"
)

// Chunk is 8 bytes (two ints). We use canary values to detect OOB writes.
const chunkCanaryStart = 0xDEADBEEF
const chunkCanaryEnd = 0xFEEDFACE

// writeCanaries sets canary values in all elements of the backing array beyond
// the slice length, so any out-of-bounds write will be detected.
func writeCanaries(backing []Chunk, sliceLen int) {
	for i := sliceLen; i < len(backing); i++ {
		backing[i].Start = chunkCanaryStart
		backing[i].End = chunkCanaryEnd
	}
}

// checkCanaries verifies that canary values beyond the slice length were not modified.
func checkCanaries(t *testing.T, name string, backing []Chunk, sliceLen int) {
	for i := sliceLen; i < len(backing); i++ {
		if backing[i].Start != chunkCanaryStart || backing[i].End != chunkCanaryEnd {
			t.Errorf("%s: out-of-bounds write at index %d (canary corrupted: Start=%x, End=%x)",
				name, i, backing[i].Start, backing[i].End)
		}
	}
}

// TestEarlyExitSplitterCanary tests the early-exit splitter with canary values.
func TestEarlyExitSplitterCanary(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		sliceLen int
		backing  int
	}{
		{"one_capacity_multi_objects", `{"a":1},{"b":2}`, 1, 3},
		{"exact_capacity", `{"a":1},{"b":2}`, 2, 4},
		{"many_objects_small_output", `{"a":1},{"b":2},{"c":3},{"d":4}`, 2, 6},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.input)
			backing := make([]Chunk, tc.backing)
			writeCanaries(backing, tc.sliceLen)
			chunks := backing[:tc.sliceLen]

			totalCount, _ := findObjectBoundariesEarlyExitASM(data, chunks)

			checkCanaries(t, tc.name, backing, tc.sliceLen)

			// Verify total count is correct
			expected := 0
			for i := 0; i < len(data); i++ {
				if data[i] == '}' {
					expected++
				}
			}
			if totalCount != expected {
				t.Errorf("expected %d objects, got %d", expected, totalCount)
			}
		})
	}
}

// TestRegularSplitterCanary tests the regular splitter with canary values.
func TestRegularSplitterCanary(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		sliceLen int
		backing  int
	}{
		{"one_capacity_multi_objects", `{"a":1},{"b":2}`, 1, 3},
		{"exact_capacity", `{"a":1},{"b":2}`, 2, 4},
		{"many_objects_small_output", `{"a":1},{"b":2},{"c":3},{"d":4}`, 2, 6},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.input)
			backing := make([]Chunk, tc.backing)
			writeCanaries(backing, tc.sliceLen)
			chunks := backing[:tc.sliceLen]

			totalCount, _ := findObjectBoundariesASM(data, chunks)

			checkCanaries(t, tc.name, backing, tc.sliceLen)

			// Verify total count is correct
			expected := 0
			for i := 0; i < len(data); i++ {
				if data[i] == '}' {
					expected++
				}
			}
			if totalCount != expected {
				t.Errorf("expected %d objects, got %d", expected, totalCount)
			}
		})
	}
}

// TestSplitterZeroLengthNonNilBacking tests output length 0 with non-nil backing.
func TestSplitterZeroLengthNonNilBacking(t *testing.T) {
	data := []byte(`{"a":1},{"b":2}`)
	backing := make([]Chunk, 3)
	writeCanaries(backing, 0)
	chunks := backing[:0]

	// Early-exit version
	totalCount, _ := findObjectBoundariesEarlyExitASM(data, chunks)
	checkCanaries(t, "early_exit_zero_len", backing, 0)
	if totalCount != 2 {
		t.Errorf("early exit: expected 2 objects, got %d", totalCount)
	}

	// Regular version
	writeCanaries(backing, 0)
	totalCount, _ = findObjectBoundariesASM(data, chunks)
	checkCanaries(t, "regular_zero_len", backing, 0)
	if totalCount != 2 {
		t.Errorf("regular: expected 2 objects, got %d", totalCount)
	}
}

// TestSplitterScalarTailFull tests that when output becomes full during scalar-tail
// processing, no OOB write occurs. Uses input shorter than one SIMD block (32 bytes)
// to force the scalar-tail path.
func TestSplitterScalarTailFull(t *testing.T) {
	// Input is 19 bytes, well under 32-byte SIMD block -> scalar tail only
	data := []byte(`{"a":1},{"b":2}`)

	for _, splitter := range []struct {
		name string
		fn   func([]byte, []Chunk) (int, int)
	}{
		{"early_exit", findObjectBoundariesEarlyExitASM},
		{"regular", findObjectBoundariesASM},
	} {
		t.Run(splitter.name+"_scalar_tail_full", func(t *testing.T) {
			backing := make([]Chunk, 3)
			writeCanaries(backing, 1)
			chunks := backing[:1]

			totalCount, _ := splitter.fn(data, chunks)

			checkCanaries(t, splitter.name+"_scalar_tail", backing, 1)
			if totalCount != 2 {
				t.Errorf("expected 2 objects, got %d", totalCount)
			}
		})
	}
}

// TestSplitterExactCapacityScalarTail tests exact output capacity with scalar-tail path.
func TestSplitterExactCapacityScalarTail(t *testing.T) {
	// Input is 19 bytes -> scalar tail only
	data := []byte(`{"a":1},{"b":2}`)

	for _, splitter := range []struct {
		name string
		fn   func([]byte, []Chunk) (int, int)
	}{
		{"early_exit", findObjectBoundariesEarlyExitASM},
		{"regular", findObjectBoundariesASM},
	} {
		t.Run(splitter.name+"_exact_capacity_scalar", func(t *testing.T) {
			backing := make([]Chunk, 4)
			writeCanaries(backing, 2)
			chunks := backing[:2]

			totalCount, _ := splitter.fn(data, chunks)

			checkCanaries(t, splitter.name+"_exact_scalar", backing, 2)
			if totalCount != 2 {
				t.Errorf("expected 2 objects, got %d", totalCount)
			}
		})
	}
}

// TestSplitterMoreObjectsThanCapacity tests input with more objects than output can hold.
func TestSplitterMoreObjectsThanCapacity(t *testing.T) {
	// 4 objects in 38 bytes -> scalar tail only
	data := []byte(`{"a":1},{"b":2},{"c":3},{"d":4}`)

	for _, splitter := range []struct {
		name string
		fn   func([]byte, []Chunk) (int, int)
	}{
		{"early_exit", findObjectBoundariesEarlyExitASM},
		{"regular", findObjectBoundariesASM},
	} {
		t.Run(splitter.name+"_more_than_capacity", func(t *testing.T) {
			backing := make([]Chunk, 6)
			writeCanaries(backing, 2)
			chunks := backing[:2]

			totalCount, _ := splitter.fn(data, chunks)

			checkCanaries(t, splitter.name+"_more_than_cap", backing, 2)
			if totalCount != 4 {
				t.Errorf("expected 4 objects, got %d", totalCount)
			}
		})
	}
}

// TestSplitterSimdToScalarTransition tests the transition from SIMD loop to scalar tail.
// Uses input longer than one SIMD block (32 bytes) with closing brace in the scalar tail.
func TestSplitterSimdToScalarTransition(t *testing.T) {
	// 45 bytes: first 32 bytes processed by SIMD, remaining 13 by scalar tail
	// The last object's closing brace is in the scalar tail region
	data := []byte(`{"a":1},{"b":2},{"c":3},{"d":4,"extra":"padding to make it long"}`)

	// Early-exit version: exits when output capacity reached (after 2 stores, count=3)
	t.Run("early_exit_simd_to_scalar", func(t *testing.T) {
		backing := make([]Chunk, 6)
		writeCanaries(backing, 2)
		chunks := backing[:2]

		totalCount, _ := findObjectBoundariesEarlyExitASM(data, chunks)

		checkCanaries(t, "early_exit_simd_scalar", backing, 2)
		if totalCount != 3 {
			t.Errorf("expected 3 objects (early exit at capacity), got %d", totalCount)
		}
	})

	// Regular version: processes all input regardless of output capacity
	t.Run("regular_simd_to_scalar", func(t *testing.T) {
		backing := make([]Chunk, 6)
		writeCanaries(backing, 2)
		chunks := backing[:2]

		totalCount, _ := findObjectBoundariesASM(data, chunks)

		checkCanaries(t, "regular_simd_scalar", backing, 2)
		if totalCount != 4 {
			t.Errorf("expected 4 objects, got %d", totalCount)
		}
	})
}
