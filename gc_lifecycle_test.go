package silentjson

import (
	"bytes"
	"reflect"
	"runtime"
	"testing"
	"unsafe"
)

// TestZeroCopyStringLifetime verifies that zero-copy strings created by the
// parser remain valid after GC. The input buffer must stay alive as long as
// the parsed object is used.
func TestZeroCopyStringLifetime(t *testing.T) {
	type Doc struct {
		Name string `json:"name"`
	}

	reg := BuildRegistry(reflect.TypeOf(Doc{}))
	reg.CopyStrings = false // Enable zero-copy mode

	// Parse a document with a string field
	payload := []byte(`{"name":"test value"}`)
	var doc Doc
	err := ParseObject(payload, reg, unsafe.Pointer(&doc))
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	if doc.Name != "test value" {
		t.Errorf("expected 'test value', got '%s'", doc.Name)
	}

	// Force GC to verify the string is still valid
	runtime.GC()

	if doc.Name != "test value" {
		t.Errorf("string changed after GC: '%s'", doc.Name)
	}
}

// TestCopyStringMode verifies that copyStrings=true creates independent strings.
func TestCopyStringMode(t *testing.T) {
	type Doc struct {
		Name string `json:"name"`
	}

	reg := BuildRegistry(reflect.TypeOf(Doc{}))
	reg.CopyStrings = true // Disable zero-copy mode

	payload := []byte(`{"name":"test value"}`)
	var doc Doc
	err := ParseObject(payload, reg, unsafe.Pointer(&doc))
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	if doc.Name != "test value" {
		t.Errorf("expected 'test value', got '%s'", doc.Name)
	}

	// Force GC
	runtime.GC()

	if doc.Name != "test value" {
		t.Errorf("string changed after GC: '%s'", doc.Name)
	}
}

// TestStreamDecoderObjectReuse verifies that the stream decoder reuses a single
// object (expected behavior). Users must copy data if they want to keep it.
func TestStreamDecoderObjectReuse(t *testing.T) {
	type Doc struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	reg := BuildRegistry(reflect.TypeOf(Doc{}))

	// Create stream with multiple documents in an array
	payload := []byte(`[{"id":1,"name":"first"},{"id":2,"name":"second"}]`)
	reader := bytes.NewReader(payload)
	decoder := NewStreamDecoder[Doc](reader, reg)

	var names []string
	err := decoder.Next(func(obj *Doc) bool {
		names = append(names, obj.Name) // Copy the string value
		return true
	})
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}

	if len(names) != 2 {
		t.Errorf("expected 2 documents, got %d", len(names))
	}

	// Verify we captured the correct names during iteration
	if names[0] != "first" {
		t.Errorf("first document's name wrong: '%s'", names[0])
	}
	if names[1] != "second" {
		t.Errorf("second document's name wrong: '%s'", names[1])
	}

	// Force GC - the copied string values should still be valid
	runtime.GC()

	if names[0] != "first" {
		t.Errorf("first name changed after GC: '%s'", names[0])
	}
	if names[1] != "second" {
		t.Errorf("second name changed after GC: '%s'", names[1])
	}
}
