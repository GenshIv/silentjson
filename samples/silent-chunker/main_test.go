package main

import (
	"bytes"
	"io"
	"reflect"
	"testing"

	"github.com/GenshIv/silentjson/v2"
)

func TestStreamDecoderNextRaw(t *testing.T) {
	// A small array with 5 objects
	inputJSON := []byte(`[
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
		{"id": 3, "name": "Charlie"},
		{"id": 4, "name": "David"},
		{"id": 5, "name": "Eve"}
	]`)

	type Dummy struct{}
	reg := silentjson.BuildRegistry(reflect.TypeOf(Dummy{}))
	dec := silentjson.NewStreamDecoder[Dummy](bytes.NewReader(inputJSON), reg)

	count := 0
	for {
		raw, countObjects, err := dec.NextRawBlock(1, 0)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if countObjects != 1 {
			t.Fatalf("Expected 1 object, got %d", countObjects)
		}
		if len(raw) == 0 {
			t.Fatalf("Expected non-empty raw bytes")
		}
		count++
	}

	if count != 5 {
		t.Errorf("Expected to extract 5 objects, got %d", count)
	}
}

func BenchmarkStreamDecoderNextRaw(b *testing.B) {
	// Let's create a reasonably sized JSON array in memory
	var buf bytes.Buffer
	buf.WriteString("[")
	for i := 0; i < 10000; i++ {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(`{"id": 12345, "name": "Benchmark User", "active": true, "balance": 999.99, "roles": ["admin", "user"]}`)
	}
	buf.WriteString("]")
	payload := buf.Bytes()

	type Dummy struct{}
	reg := silentjson.BuildRegistry(reflect.TypeOf(Dummy{}))

	b.ResetTimer()
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dec := silentjson.NewStreamDecoder[Dummy](bytes.NewReader(payload), reg)
		for {
			_, _, err := dec.NextRawBlock(1000, 0)
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatalf("Stream error: %v", err)
			}
		}
	}
}
