package silentjson

import (
	"bytes"
	"io"
	"reflect"
	"testing"
)

func TestStreamDecoder_Basic(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))

	payload := []byte(`[
		{"id": 1, "name": "Alice", "active": true},
		{"id": 2, "name": "Bob", "active": false},
		{"id": 3, "name": "Charlie", "active": true}
	]`)

	// Simulate streaming with a very small buffer to force boundary chunks
	r := bytes.NewReader(payload)
	dec := NewStreamDecoder[TestWorkerItem](r, reg)
	
	// Force small internal buffer for testing chunking
	dec.buf = make([]byte, 10) 

	var results []TestWorkerItem
	for {
		var item TestWorkerItem
		err := dec.Decode(&item)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		results = append(results, item)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 items, got %d", len(results))
	}
	if results[0].Name != "Alice" || results[1].Name != "Bob" || results[2].Name != "Charlie" {
		t.Errorf("data mismatch: %+v", results)
	}
}

func TestStreamDecoder_NoArray(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`{"id": 1, "name": "Alice"}`)
	r := bytes.NewReader(payload)
	dec := NewStreamDecoder[TestWorkerItem](r, reg)

	var item TestWorkerItem
	err := dec.Decode(&item)
	if err == nil {
		t.Fatal("expected error for missing array bracket, got nil")
	}
}

func TestStreamDecoder_IncompleteObject(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[{"id": 1, "name": "Al`)
	r := bytes.NewReader(payload)
	dec := NewStreamDecoder[TestWorkerItem](r, reg)

	var item TestWorkerItem
	err := dec.Decode(&item)
	if err != ErrUnexpectedEOF {
		t.Fatalf("expected ErrUnexpectedEOF, got %v", err)
	}
}
