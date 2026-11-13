package silentjson

import (
	"testing"
)

// TestObjectSplitterCapacityBoundary tests that the object splitter correctly
// handles output slices at exact capacity boundaries without writing past the end.
func TestObjectSplitterCapacityBoundary(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		capacity int
	}{
		{"empty_output", `{"a":1}`, 0},
		{"one_element_capacity", `{"a":1},{"b":2}`, 1},
		{"exact_capacity", `{"a":1},{"b":2}`, 2},
		{"capacity_less_than_elements", `{"a":1},{"b":2},{"c":3}`, 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.input)
			chunks := make([]Chunk, tc.capacity)

			totalCount, _ := findObjectBoundariesEarlyExitASM(data, chunks)

			// Verify we didn't write past capacity
			storedCount := 0
			for _, chunk := range chunks {
				if chunk.Start == -1 && chunk.End == -1 {
					break
				}
				storedCount++
			}

			if storedCount > tc.capacity {
				t.Errorf("wrote %d chunks to capacity-%d slice", storedCount, tc.capacity)
			}

			// Verify total count is correct regardless of capacity
			expected := 0
			for i := 0; i < len(data); i++ {
				if data[i] == '}' && i > 0 && data[i-1] != '\\' {
					expected++
				}
			}
			if totalCount != expected {
				t.Errorf("expected %d objects, got %d", expected, totalCount)
			}
		})
	}
}

// TestObjectSplitterRegularCapacityBoundary tests the regular (non-early-exit) splitter.
func TestObjectSplitterRegularCapacityBoundary(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		capacity int
	}{
		{"empty_output", `{"a":1}`, 0},
		{"one_element_capacity", `{"a":1},{"b":2}`, 1},
		{"exact_capacity", `{"a":1},{"b":2}`, 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.input)
			chunks := make([]Chunk, tc.capacity)

			totalCount, _ := findObjectBoundariesASM(data, chunks)

			storedCount := 0
			for _, chunk := range chunks {
				if chunk.Start == -1 && chunk.End == -1 {
					break
				}
				storedCount++
			}

			if storedCount > tc.capacity {
				t.Errorf("wrote %d chunks to capacity-%d slice", storedCount, tc.capacity)
			}

			expected := 0
			for i := 0; i < len(data); i++ {
				if data[i] == '}' && i > 0 && data[i-1] != '\\' {
					expected++
				}
			}
			if totalCount != expected {
				t.Errorf("expected %d objects, got %d", expected, totalCount)
			}
		})
	}
}
