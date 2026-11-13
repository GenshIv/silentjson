package silentjson

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"
)

// Test deep nesting doesn't cause stack overflow
func TestDeepNesting(t *testing.T) {
	depth := 100
	jsonStr := "{"
	for i := 0; i < depth; i++ {
		jsonStr += fmt.Sprintf(`"l%d":{`, i)
	}
	jsonStr += `"leaf":42`
	for i := 0; i < depth; i++ {
		jsonStr += "}"
	}
	jsonStr += "}"

	// This should parse without stack overflow
	err := ParseObject([]byte(jsonStr), BuildRegistry(reflect.TypeOf(map[string]interface{}{})), nil)
	// We expect an error because map[string]interface{} isn't supported,
	// but it shouldn't panic or crash
	if err == nil {
		t.Log("deep nesting parsed successfully")
	} else {
		t.Logf("deep nesting error (expected for unsupported type): %v", err)
	}
}

// Test very large array
func TestLargeArray(t *testing.T) {
	n := 10000
	jsonStr := "["
	for i := 0; i < n; i++ {
		if i > 0 {
			jsonStr += ","
		}
		jsonStr += fmt.Sprintf(`{"id":%d,"name":"item%d"}`, i, i)
	}
	jsonStr += "]"

	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	reg := BuildRegistry(reflect.TypeOf(Item{}))
	dst := make([]Item, n)
	result, err := UnmarshalSlice([]byte(jsonStr), reg, dst)
	if err != nil {
		t.Fatalf("large array parse failed: %v", err)
	}
	if len(result) != n {
		t.Errorf("expected %d items, got %d", n, len(result))
	}
	if result[0].ID != 0 || result[n-1].ID != n-1 {
		t.Errorf("first/last item wrong: %+v / %+v", result[0], result[n-1])
	}
}

// Test truncated input at various points
func TestTruncatedInput(t *testing.T) {
	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	reg := BuildRegistry(reflect.TypeOf(Item{}))

	testCases := []struct {
		name  string
		input string
	}{
		{"truncated object", `{"id":1`},
		{"truncated value", `{"id":`},
		{"truncated string", `{"name":"abc`},
		{"truncated array", `[{"id":1}`},
		{"empty input", ``},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var item Item
			err := ParseObject([]byte(tc.input), reg, unsafe.Pointer(&item))
			if err == nil {
				t.Errorf("expected error for truncated input %q", tc.input)
			}
		})
	}
}

// Test repeated decoding into same destination
func TestRepeatedDecoding(t *testing.T) {
	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Tags []string `json:"tags"`
	}
	reg := BuildRegistry(reflect.TypeOf(Item{}))

	var item Item
	for i := 0; i < 10; i++ {
		jsonStr := fmt.Sprintf(`{"id":%d,"name":"item%d","tags":["tag%d"]}`, i, i, i)
		err := ParseObject([]byte(jsonStr), reg, unsafe.Pointer(&item))
		if err != nil {
			t.Fatalf("iteration %d failed: %v", i, err)
		}
		if item.ID != i || item.Name != fmt.Sprintf("item%d", i) {
			t.Errorf("iteration %d wrong values: %+v", i, item)
		}
		if len(item.Tags) != 1 || item.Tags[0] != fmt.Sprintf("tag%d", i) {
			t.Errorf("iteration %d wrong tags: %+v", i, item.Tags)
		}
	}
}
