package silentjson

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"
)

type SliceFields struct {
	Name string   `json:"name"`
	Ints []int    `json:"ints"`
	Strs []string `json:"strs"`
}

func TestSlice_EmptyArrayTruncates(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SliceFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","ints":[1,2,3],"strs":["a","b"]}`)
	var obj SliceFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}
	if len(obj.Ints) != 3 || len(obj.Strs) != 2 {
		t.Fatalf("first parse wrong lengths: ints=%d strs=%d", len(obj.Ints), len(obj.Strs))
	}

	// Reuse destination with empty arrays
	payload2 := []byte(`{"name":"second","ints":[],"strs":[]}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if len(obj.Ints) != 0 {
		t.Errorf("empty ints array should truncate, got len=%d, values=%v", len(obj.Ints), obj.Ints)
	}
	if len(obj.Strs) != 0 {
		t.Errorf("empty strs array should truncate, got len=%d, values=%v", len(obj.Strs), obj.Strs)
	}
}

func TestSlice_NullSetsNil(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SliceFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","ints":[1,2,3],"strs":["a","b"]}`)
	var obj SliceFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Reuse destination with null slices
	payload2 := []byte(`{"name":"second","ints":null,"strs":null}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if obj.Ints != nil {
		t.Errorf("null ints should set slice to nil, got len=%d cap=%d values=%v", len(obj.Ints), cap(obj.Ints), obj.Ints)
	}
	if obj.Strs != nil {
		t.Errorf("null strs should set slice to nil, got len=%d cap=%d values=%v", len(obj.Strs), cap(obj.Strs), obj.Strs)
	}
}

type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type SliceStructFields struct {
	Name  string  `json:"name"`
	Items []Item  `json:"items"`
}

type SlicePtrStructFields struct {
	Name  string     `json:"name"`
	Items []*Item    `json:"items"`
}

func TestSliceStruct_NullSetsNil(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SliceStructFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	var obj SliceStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}
	if len(obj.Items) != 2 {
		t.Fatalf("first parse wrong length: %d", len(obj.Items))
	}

	// Reuse destination with null slice of structs
	payload2 := []byte(`{"name":"second","items":null}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if obj.Items != nil {
		t.Errorf("null items should set slice to nil, got len=%d cap=%d", len(obj.Items), cap(obj.Items))
	}
}

func TestSliceStruct_EmptyArrayTruncates(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SliceStructFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	var obj SliceStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Reuse destination with empty array
	payload2 := []byte(`{"name":"second","items":[]}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if len(obj.Items) != 0 {
		t.Errorf("empty items array should truncate, got len=%d cap=%d", len(obj.Items), cap(obj.Items))
	}
}

func TestSlicePtrStruct_NullSetsNil(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}
	if len(obj.Items) != 2 {
		t.Fatalf("first parse wrong length: %d", len(obj.Items))
	}

	// Reuse destination with null slice of ptr-structs
	payload2 := []byte(`{"name":"second","items":null}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if obj.Items != nil {
		t.Errorf("null items should set slice to nil, got len=%d cap=%d", len(obj.Items), cap(obj.Items))
	}
}

func TestSlicePtrStruct_EmptyArrayTruncates(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Reuse destination with empty array
	payload2 := []byte(`{"name":"second","items":[]}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if len(obj.Items) != 0 {
		t.Errorf("empty items array should truncate, got len=%d cap=%d", len(obj.Items), cap(obj.Items))
	}
}

func TestSlicePtrStruct_NullThenNonEmpty(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Decode null
	payload2 := []byte(`{"name":"second","items":null}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("null parse failed: %v", err)
	}
	if obj.Items != nil {
		t.Errorf("null should set slice to nil, got len=%d", len(obj.Items))
	}

	// Decode non-empty array again
	payload3 := []byte(`{"name":"third","items":[{"id":3,"name":"c"}]}`)
	err = ParseObject(payload3, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("third parse failed: %v", err)
	}
	if len(obj.Items) != 1 || obj.Items[0].ID != 3 || obj.Items[0].Name != "c" {
		t.Errorf("expected single item after null, got %+v", obj.Items)
	}
}

func TestSlicePtrStruct_EmptyThenNonEmpty(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Decode empty array
	payload2 := []byte(`{"name":"second","items":[]}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("empty parse failed: %v", err)
	}
	if len(obj.Items) != 0 {
		t.Errorf("empty should truncate slice, got len=%d", len(obj.Items))
	}

	// Decode non-empty array again
	payload3 := []byte(`{"name":"third","items":[{"id":3,"name":"c"}]}`)
	err = ParseObject(payload3, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("third parse failed: %v", err)
	}
	if len(obj.Items) != 1 || obj.Items[0].ID != 3 || obj.Items[0].Name != "c" {
		t.Errorf("expected single item after empty, got %+v", obj.Items)
	}
}

func TestSlicePtrStruct_EmptyClearsStalePointers(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Decode empty array
	payload2 := []byte(`{"name":"second","items":[]}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("empty parse failed: %v", err)
	}
	if len(obj.Items) != 0 {
		t.Errorf("empty should truncate slice, got len=%d", len(obj.Items))
	}

	// Inspect backing array up to capacity for stale pointers
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&obj.Items))
	base := unsafe.Pointer(hdr.Data)
	elemSize := hdr.Cap * 8 // pointer size (64-bit)
	if hdr.Cap > 0 {
		raw := unsafe.Slice((*byte)(base), elemSize)
		for i := 0; i < hdr.Cap; i++ {
			slot := raw[i*8 : (i+1)*8]
			var ptrVal uint64
			for j := 0; j < 8; j++ {
				ptrVal = ptrVal<<8 | uint64(slot[j])
			}
			if ptrVal != 0 {
				t.Errorf("stale pointer at index %d in backing array after empty decode", i)
			}
		}
	}
}

func TestSlicePtrStruct_ShorterArrayClearsStalePointers(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First decode with 3 values
	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"},{"id":3,"name":"c"}]}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Decode shorter array (2 elements)
	payload2 := []byte(`{"name":"second","items":[{"id":4,"name":"d"},{"id":5,"name":"e"}]}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("shorter parse failed: %v", err)
	}
	if len(obj.Items) != 2 || obj.Items[0].ID != 4 || obj.Items[1].ID != 5 {
		t.Errorf("expected 2 items after shorter decode, got %+v", obj.Items)
	}

	// Inspect backing array: indices 0 and 1 should have new values, index 2+ should be nil
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&obj.Items))
	base := unsafe.Pointer(hdr.Data)
	for i := 0; i < hdr.Cap; i++ {
		slotPtr := unsafe.Pointer(uintptr(base) + uintptr(i)*8)
		ptrVal := *(*unsafe.Pointer)(slotPtr)
		if i < 2 {
			if ptrVal == nil {
				t.Errorf("expected non-nil pointer at index %d", i)
			}
		} else {
			if ptrVal != nil {
				t.Errorf("stale pointer at index %d in backing array after shorter decode", i)
			}
		}
	}
}

func TestSlicePtrStruct_RepeatedDecoding(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	var obj SlicePtrStructFields
	for i := 0; i < 5; i++ {
		n := 3 - (i % 2) // alternate between 3 and 2 elements
		jsonStr := fmt.Sprintf(`{"name":"iter%d","items":[`, i)
		for j := 0; j < n; j++ {
			if j > 0 {
				jsonStr += ","
			}
			jsonStr += fmt.Sprintf(`{"id":%d,"name":"item%d"}`, i*10+j, i*10+j)
		}
		jsonStr += "]}"

		err := ParseObject([]byte(jsonStr), reg, unsafe.Pointer(&obj))
		if err != nil {
			t.Fatalf("iteration %d parse failed: %v", i, err)
		}
		if len(obj.Items) != n {
			t.Errorf("iteration %d: expected %d items, got %d", i, n, len(obj.Items))
		}
		for j := 0; j < n; j++ {
			if obj.Items[j].ID != i*10+j {
				t.Errorf("iteration %d item %d: wrong ID %d", i, j, obj.Items[j].ID)
			}
		}
	}
}

func TestSliceStruct_EmptyDoesNotClearValueSlots(t *testing.T) {
	// Verify that non-pointer struct slices are NOT affected by the pointer-clearing logic.
	// Value slots should remain as-is (no zeroing).
	reg := BuildRegistry(reflect.TypeOf(SliceStructFields{}))

	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	var obj SliceStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	payload2 := []byte(`{"name":"second","items":[]}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("empty parse failed: %v", err)
	}
	if len(obj.Items) != 0 {
		t.Errorf("empty should truncate slice, got len=%d", len(obj.Items))
	}
}

func TestSlicePtrStruct_MalformedSecondElement(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First element valid, second element truncated mid-parse
	payload := []byte(`{"name":"test","items":[{"id":1,"name":"a"},{"id":2`)
	var obj SlicePtrStructFields
	err := ParseObject(payload, reg, unsafe.Pointer(&obj))
	if err == nil {
		t.Fatal("expected parse error for malformed second element")
	}

	// After error, slice should have 1 element (first element parsed successfully)
	if len(obj.Items) != 1 {
		t.Errorf("expected 1 element after parse error, got %d", len(obj.Items))
	}
	if obj.Items[0].ID != 1 || obj.Items[0].Name != "a" {
		t.Errorf("first element should be preserved: %+v", obj.Items[0])
	}

	// Verify no stale pointers beyond the logical slice length
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&obj.Items))
	if hdr.Cap > 1 {
		base := unsafe.Pointer(hdr.Data)
		for j := 1; j < hdr.Cap; j++ {
			slot := unsafe.Pointer(uintptr(base) + uintptr(j)*8)
			ptrVal := *(*unsafe.Pointer)(slot)
			if ptrVal != nil {
				t.Errorf("stale pointer at index %d after parse error", j)
			}
		}
	}
}

func TestSlicePtrStruct_MalformedFirstElement(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First element truncated mid-parse
	payload := []byte(`{"name":"test","items":[{"id":1`)
	var obj SlicePtrStructFields
	err := ParseObject(payload, reg, unsafe.Pointer(&obj))
	if err == nil {
		t.Fatal("expected parse error for malformed first element")
	}

	// After error, slice should have 0 elements (no elements parsed successfully)
	if len(obj.Items) != 0 {
		t.Errorf("expected 0 elements after parse error, got %d", len(obj.Items))
	}

	// Verify no stale pointers in backing array
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&obj.Items))
	if hdr.Cap > 0 {
		base := unsafe.Pointer(hdr.Data)
		for j := 0; j < hdr.Cap; j++ {
			slot := unsafe.Pointer(uintptr(base) + uintptr(j)*8)
			ptrVal := *(*unsafe.Pointer)(slot)
			if ptrVal != nil {
				t.Errorf("stale pointer at index %d after parse error", j)
			}
		}
	}
}

func TestSlicePtrStruct_MalformedInputOnReusedSlice(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First decode with valid values
	payload1 := []byte(`{"name":"first","items":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Now decode malformed input into the same destination (truncated mid-parse)
	payload2 := []byte(`{"name":"second","items":[{"id":3`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err == nil {
		t.Fatal("expected parse error for malformed input")
	}

	// After error, slice should have 0 elements (no elements parsed successfully in second decode)
	if len(obj.Items) != 0 {
		t.Errorf("expected 0 elements after parse error, got %d", len(obj.Items))
	}

	// Verify no stale pointers in backing array (old values from first decode cleared)
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&obj.Items))
	if hdr.Cap > 0 {
		base := unsafe.Pointer(hdr.Data)
		for j := 0; j < hdr.Cap; j++ {
			slot := unsafe.Pointer(uintptr(base) + uintptr(j)*8)
			ptrVal := *(*unsafe.Pointer)(slot)
			if ptrVal != nil {
				t.Errorf("stale pointer at index %d after parse error on reused slice", j)
			}
		}
	}
}

func TestSlicePtrStruct_TruncatedInput(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// Truncated input (unexpected EOF) - first element valid, no closing bracket
	payload := []byte(`{"name":"test","items":[{"id":1,"name":"a"}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload, reg, unsafe.Pointer(&obj))
	if err == nil {
		t.Fatal("expected parse error for truncated input")
	}

	// After error, slice should have 1 element (first element parsed successfully)
	if len(obj.Items) != 1 {
		t.Errorf("expected 1 element after parse error, got %d", len(obj.Items))
	}
	if obj.Items[0].ID != 1 || obj.Items[0].Name != "a" {
		t.Errorf("first element should be preserved: %+v", obj.Items[0])
	}

	// Verify no stale pointers beyond the logical slice length
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&obj.Items))
	if hdr.Cap > 1 {
		base := unsafe.Pointer(hdr.Data)
		for j := 1; j < hdr.Cap; j++ {
			slot := unsafe.Pointer(uintptr(base) + uintptr(j)*8)
			ptrVal := *(*unsafe.Pointer)(slot)
			if ptrVal != nil {
				t.Errorf("stale pointer at index %d after truncated input", j)
			}
		}
	}
}

func TestSlice_MissingFieldPreserves(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SliceFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","ints":[1,2,3],"strs":["a","b"]}`)
	var obj SliceFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Decode without the slice fields
	payload2 := []byte(`{"name":"second"}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	// Missing fields should preserve existing values
	if len(obj.Ints) != 3 {
		t.Errorf("missing ints field should preserve existing slice, got len=%d", len(obj.Ints))
	}
	if len(obj.Strs) != 2 {
		t.Errorf("missing strs field should preserve existing slice, got len=%d", len(obj.Strs))
	}
}

func TestSlice_NonEmptyReplaces(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SliceFields{}))

	// First decode with values
	payload1 := []byte(`{"name":"first","ints":[1,2,3],"strs":["a","b"]}`)
	var obj SliceFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Decode with different values
	payload2 := []byte(`{"name":"second","ints":[4],"strs":["c"]}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if !reflect.DeepEqual(obj.Ints, []int{4}) {
		t.Errorf("ints should be replaced, got %v", obj.Ints)
	}
	if !reflect.DeepEqual(obj.Strs, []string{"c"}) {
		t.Errorf("strs should be replaced, got %v", obj.Strs)
	}
}

// Test null elements within []*Struct arrays (field context)
func TestSlicePtrStruct_NullElementsInArray(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	payload := []byte(`{"name":"test","items":[{"id":1,"name":"a"},null,{"id":3,"name":"c"}]}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(obj.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(obj.Items))
	}
	if obj.Items[0] == nil || obj.Items[0].ID != 1 {
		t.Errorf("first item should be non-nil with id=1")
	}
	if obj.Items[1] != nil {
		t.Errorf("second item should be nil, got %+v", obj.Items[1])
	}
	if obj.Items[2] == nil || obj.Items[2].ID != 3 {
		t.Errorf("third item should be non-nil with id=3")
	}
}

// Test null elements within root slice parsing
func TestRootSlicePtr_NullElementsInArray(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf([]*Item{}))

	payload := []byte(`[{"id":1,"name":"a"},null,{"id":3,"name":"c"}]`)
	var items []*Item
	err := ParseObject(payload, reg, unsafe.Pointer(&items))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0] == nil || items[0].ID != 1 {
		t.Errorf("first item should be non-nil with id=1")
	}
	if items[1] != nil {
		t.Errorf("second item should be nil, got %+v", items[1])
	}
	if items[2] == nil || items[2].ID != 3 {
		t.Errorf("third item should be non-nil with id=3")
	}
}

// Test that re-decoding with shorter array clears stale pointers (root slice context)
func TestRootSlicePtr_ShorterArrayClearsStale(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf([]*Item{}))

	// First decode with two items
	payload1 := []byte(`[{"id":1,"name":"a"},{"id":2,"name":"b"}]`)
	var items []*Item
	err := ParseObject(payload1, reg, unsafe.Pointer(&items))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Re-decode with shorter array (one item)
	payload2 := []byte(`[{"id":3,"name":"c"}]`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&items))
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item after shorter decode, got %d", len(items))
	}
	if items[0] == nil || items[0].ID != 3 {
		t.Errorf("first item should be id=3")
	}

	// Check that the second slot is cleared (stale pointer removed)
	inspectSlicePtr := func(slice []*Item) {
		header := (*reflect.SliceHeader)(unsafe.Pointer(&slice))
		base := unsafe.Pointer(header.Data)
		elemSize := unsafe.Sizeof((*Item)(nil))
		for j := 0; j < header.Cap; j++ {
			slot := unsafe.Pointer(uintptr(base) + uintptr(j)*elemSize)
			ptr := *(*unsafe.Pointer)(slot)
			if j == 0 && ptr == nil {
				t.Errorf("slot %d should be non-nil", j)
			}
			if j >= header.Len && ptr != nil {
				t.Errorf("slot %d (beyond len) should be nil, got non-nil stale pointer", j)
			}
		}
	}
	inspectSlicePtr(items)
}

// Test root null sets slice to nil
func TestRootSlice_NullSetsNil(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf([]*Item{}))

	// First decode with items
	payload1 := []byte(`[{"id":1,"name":"a"}]`)
	var items []*Item
	err := ParseObject(payload1, reg, unsafe.Pointer(&items))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	// Decode null - should set slice to nil
	payload2 := []byte(`null`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&items))
	if err != nil {
		t.Fatalf("null parse failed: %v", err)
	}
	if items != nil {
		t.Errorf("slice should be nil after null decode, got len=%d", len(items))
	}
}

// Test root null sets object slice field to nil
func TestSliceField_NullSetsNil(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SlicePtrStructFields{}))

	// First decode with items
	payload1 := []byte(`{"name":"test","items":[{"id":1,"name":"a"}]}`)
	var obj SlicePtrStructFields
	err := ParseObject(payload1, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}
	if len(obj.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(obj.Items))
	}

	// Decode null for items field - should set slice to nil
	payload2 := []byte(`{"name":"test","items":null}`)
	err = ParseObject(payload2, reg, unsafe.Pointer(&obj))
	if err != nil {
		t.Fatalf("null parse failed: %v", err)
	}
	if obj.Items != nil {
		t.Errorf("items slice should be nil after null decode, got len=%d", len(obj.Items))
	}
}
