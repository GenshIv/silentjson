package silentjson

import (
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

type SliceStructFields struct {
	Name string `json:"name"`
	Items []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"items"`
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
