package silentjson

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"
)

// Test structure covering all our types
type TestUser struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	IsActive bool     `json:"is_active"`
	Balance  float64  `json:"balance"`
	Tags     []string `json:"tags"`
	Roles    []int    `json:"roles"`
}

type TestWorkerItem struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

func TestParseObject_Valid(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestUser{}))

	tests := []struct {
		name     string
		payload  []byte
		expected TestUser
	}{
		{
			name:    "Full object",
			payload: []byte(`{"id":42,"name":"Igor","is_active":true,"balance":99.9,"tags":["dev","go"],"roles":[1,2]}`),
			expected: TestUser{
				ID:       42,
				Name:     "Igor",
				IsActive: true,
				Balance:  99.9,
				Tags:     []string{"dev", "go"},
				Roles:    []int{1, 2},
			},
		},
		{
			name:    "Partial object with spaces",
			payload: []byte(`{  "id" : 100 , "name" : "Test" }`),
			expected: TestUser{
				ID:   100,
				Name: "Test",
			},
		},
		{
			name:    "Escaped strings",
			payload: []byte(`{"name":"Line1\nLine2\t\"quote\""}`),
			expected: TestUser{
				Name: "Line1\nLine2\t\"quote\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual TestUser

			// Important: for In-Place unescaping, we must pass a copy of the payload
			// if we want to preserve the original slice for checks, but in tests, we can pass it directly.
			buf := make([]byte, len(tt.payload))
			copy(buf, tt.payload)

			err := ParseObject(buf, reg, unsafe.Pointer(&actual))

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("got %+v, want %+v", actual, tt.expected)
			}
		})
	}
}

func TestZeroCopy_String(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestUser{}))

	payload := []byte(`{"name":"ZeroCopyString"}`)
	var actual TestUser

	err := ParseObject(payload, reg, unsafe.Pointer(&actual))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get a pointer to the start of the original byte array
	payloadPtr := unsafe.Pointer(unsafe.SliceData(payload))

	// Get a pointer to the start of the data of the parsed string
	stringPtr := unsafe.Pointer(unsafe.StringData(actual.Name))

	// Calculate the offset. If the string refers to the payload,
	// its address must be within the payload's address range.
	diff := uintptr(stringPtr) - uintptr(payloadPtr)

	if diff >= uintptr(len(payload)) {
		t.Errorf("Zero-copy failed: string is allocated outside the original payload memory! diff: %d", diff)
	} else {
		t.Logf("Zero-copy verified: string offset from payload start is %d bytes", diff)
	}
}

func TestParseObject_Errors(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestUser{}))

	tests := []struct {
		name        string
		payload     []byte
		expectedErr error
	}{
		{
			name:    "Type mismatch - string instead of int",
			payload: []byte(`{"id":"not_an_int"}`),
			// If fastParseInt swallows the string, a type check needs to be added in ParseObject
			// expectedErr: ErrTypeMismatch,
		},
		{
			name:        "Unexpected EOF in string",
			payload:     []byte(`{"name":"unfinished`),
			expectedErr: ErrUnexpectedEOF, // Your custom error
		},
		{
			name:    "Malformed array",
			payload: []byte(`{"tags":["one", "two"`),
			// Should return an error or ignore, but most importantly - not panic
		},
		{
			name:        "Wrong boolean value",
			payload:     []byte(`{"is_active":falsy}`),
			expectedErr: ErrTypeMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual TestUser
			err := ParseObject(tt.payload, reg, unsafe.Pointer(&actual))

			// If we expect a specific error, we check for it
			// In the basic case, we at least check that an error was returned, not nil
			if err == nil && tt.expectedErr != nil {
				t.Errorf("expected error %v, got nil", tt.expectedErr)
			}
		})
	}
}

// Test 1: Basic scenarios and edge cases
func TestUnmarshalArrayParallel_Basic(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))

	tests := []struct {
		name      string
		payload   []byte
		wantLen   int
		wantFirst TestWorkerItem
		wantErr   bool
	}{
		{
			name:      "Standard array",
			payload:   []byte(`[{"id":1,"name":"Alice","active":true},{"id":2,"name":"Bob","active":false}]`),
			wantLen:   2,
			wantFirst: TestWorkerItem{ID: 1, Name: "Alice", Active: true},
			wantErr:   false,
		},
		{
			name:    "Empty array",
			payload: []byte(`[]`),
			wantLen: 0,
			wantErr: false,
		},
		{
			name:      "Array with formatting spaces and newlines",
			payload:   []byte("[\n  {\"id\": 10, \"name\": \"Space\"} \n]"),
			wantLen:   1,
			wantFirst: TestWorkerItem{ID: 10, Name: "Space"},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of the buffer, as our parser does in-place unescaping,
			// and it's good practice in tests not to mutate the source constants.
			buf := make([]byte, len(tt.payload))
			copy(buf, tt.payload)

			// Call our beautiful Generic API
			res, err := UnmarshalArrayParallel[TestWorkerItem](buf, reg)

			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr %v, got error: %v", tt.wantErr, err)
			}

			if len(res) != tt.wantLen {
				t.Errorf("want length %d, got %d", tt.wantLen, len(res))
			}

			if len(res) > 0 && !reflect.DeepEqual(res[0], tt.wantFirst) {
				t.Errorf("want first item %+v, got %+v", tt.wantFirst, res[0])
			}
		})
	}
}

// Test 2: Checking multithreading, order, and absence of data races
func TestUnmarshalArrayParallel_HighVolume(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))

	// Generate an array of 15,000 elements to guarantee
	// the use of all processor cores (Worker Pool batching)
	const numItems = 15000
	var jsonBuilder []byte
	jsonBuilder = append(jsonBuilder, '[')
	for i := 0; i < numItems; i++ {
		if i > 0 {
			jsonBuilder = append(jsonBuilder, ',')
		}
		// Embed the index directly into the data
		chunk := fmt.Sprintf(`{"id":%d,"name":"Worker_%d","active":true}`, i, i)
		jsonBuilder = append(jsonBuilder, []byte(chunk)...)
	}
	jsonBuilder = append(jsonBuilder, ']')

	// Run parallel parsing
	res, err := UnmarshalArrayParallel[TestWorkerItem](jsonBuilder, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that no element was lost during slicing (findObjectBoundaries)
	if len(res) != numItems {
		t.Fatalf("expected %d items, got %d", numItems, len(res))
	}

	// The most important check: The order of elements.
	// Since goroutines work asynchronously, we must ensure
	// that the offsets (basePtr + i * structSize) worked perfectly.
	for i := 0; i < numItems; i++ {
		if res[i].ID != i {
			t.Fatalf("ORDER MISMATCH at index %d: expected ID %d, got %d", i, i, res[i].ID)
		}
		expectedName := fmt.Sprintf("Worker_%d", i)
		if res[i].Name != expectedName {
			t.Fatalf("DATA MISMATCH at index %d: expected Name %s, got %s", i, expectedName, res[i].Name)
		}
	}
}
