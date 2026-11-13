package silentjson

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// Test deep nesting doesn't cause stack overflow
func TestDeepNesting(t *testing.T) {
	type Node struct {
		Child *Node `json:"child"`
		Value int   `json:"value"`
	}

	reg := BuildRegistry(reflect.TypeOf(Node{}))
	if reg == nil {
		t.Fatal("failed to build registry for recursive type")
	}

	depth := 100
	// Build JSON: {"value":1,"child":{"value":2,"child":{...}}}
	var sb strings.Builder
	sb.WriteString(`{"value":1`)
	for i := 2; i <= depth; i++ {
		sb.WriteString(fmt.Sprintf(`,"child":{"value":%d`, i))
	}
	sb.WriteString(`,"child":null}`)
	for i := 0; i < depth-1; i++ {
		sb.WriteString("}")
	}
	jsonStr := sb.String()

	var root Node
	err := ParseObject([]byte(jsonStr), reg, unsafe.Pointer(&root))
	if err != nil {
		t.Fatalf("deep nesting parse failed at depth %d: %v", depth, err)
	}

	// Verify we actually traversed to the leaf
	current := &root
	for i := 0; i < depth; i++ {
		if current.Value != i+1 {
			t.Errorf("value mismatch at depth %d: got %d, want %d", i, current.Value, i+1)
		}
		if i < depth-1 && current.Child == nil {
			t.Fatalf("expected child at depth %d, got nil", i)
		}
		current = current.Child
	}
	if current != nil && current.Value != depth+1 {
		t.Errorf("leaf value mismatch: got %d, want %d", current.Value, depth+1)
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
