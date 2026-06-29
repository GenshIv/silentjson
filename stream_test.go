package silentjson

import (
	"bytes"
	"io"
	"reflect"
	"testing"
)

func TestStreamDecoder_Next(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`)
	
	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)
	
	var items []TestWorkerItem
	err := dec.Next(func(item *TestWorkerItem) bool {
		items = append(items, *item)
		return true
	})
	if err != nil { t.Fatalf("unexpected err: %v", err) }
	
	if len(items) != 2 { t.Fatalf("expected 2 items, got %d", len(items)) }
	if items[0].ID != 1 { t.Errorf("expected 1, got %d", items[0].ID) }
	if items[1].ID != 2 { t.Errorf("expected 2, got %d", items[1].ID) }
}

func TestStreamDecoder_NextRaw(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`)
	
	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)
	
	raw1, err := dec.NextRaw()
	if err != nil { t.Fatalf("unexpected err: %v", err) }
	if string(raw1) != `{"id":1,"name":"Alice"}` { t.Errorf("got %s", raw1) }
	
	raw2, err := dec.NextRaw()
	if err != nil { t.Fatalf("unexpected err: %v", err) }
	if string(raw2) != `{"id":2,"name":"Bob"}` { t.Errorf("got %s", raw2) }
	
	_, err = dec.NextRaw()
	if err != io.EOF { t.Errorf("expected EOF, got %v", err) }
}

func TestStreamDecoder_NextChan(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"},{"id":3,"name":"Charlie"}]`)
	
	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)
	
	ch := dec.NextChan(1)
	
	var items []TestWorkerItem
	for res := range ch {
		if res.Err != nil {
			t.Fatalf("unexpected err in channel: %v", res.Err)
		}
		t.Logf("Received item: %+v", *res.Item)
		items = append(items, *res.Item)
	}
	
	if len(items) != 3 { t.Fatalf("expected 3 items, got %d", len(items)) }
	if items[0].ID != 1 { t.Errorf("expected 1, got %d", items[0].ID) }
	if items[1].ID != 2 { t.Errorf("expected 2, got %d", items[1].ID) }
	if items[2].ID != 3 { t.Errorf("expected 3, got %d", items[2].ID) }
}

// --- Tests for arbitrary JSON array element types ---

func TestStreamDecoder_NextRaw_Strings(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`["hello","world","foo"]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	expected := []string{`"hello"`, `"world"`, `"foo"`}
	for i, exp := range expected {
		raw, err := dec.NextRaw()
		if err != nil {
			t.Fatalf("item %d: unexpected err: %v", i, err)
		}
		if string(raw) != exp {
			t.Errorf("item %d: expected %s, got %s", i, exp, string(raw))
		}
	}

	_, err := dec.NextRaw()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestStreamDecoder_NextRaw_Numbers(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[1, 42, 999]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	expected := []string{"1", "42", "999"}
	for i, exp := range expected {
		raw, err := dec.NextRaw()
		if err != nil {
			t.Fatalf("item %d: unexpected err: %v", i, err)
		}
		if string(raw) != exp {
			t.Errorf("item %d: expected %s, got %s", i, exp, string(raw))
		}
	}

	_, err := dec.NextRaw()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestStreamDecoder_NextRaw_Floats(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[1.5, -3.14, 2.0e10]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	expected := []string{"1.5", "-3.14", "2.0e10"}
	for i, exp := range expected {
		raw, err := dec.NextRaw()
		if err != nil {
			t.Fatalf("item %d: unexpected err: %v", i, err)
		}
		if string(raw) != exp {
			t.Errorf("item %d: expected %s, got %s", i, exp, string(raw))
		}
	}

	_, err := dec.NextRaw()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestStreamDecoder_NextRaw_Booleans(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[true, false, true]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	expected := []string{"true", "false", "true"}
	for i, exp := range expected {
		raw, err := dec.NextRaw()
		if err != nil {
			t.Fatalf("item %d: unexpected err: %v", i, err)
		}
		if string(raw) != exp {
			t.Errorf("item %d: expected %s, got %s", i, exp, string(raw))
		}
	}

	_, err := dec.NextRaw()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestStreamDecoder_NextRaw_Nulls(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[null, null]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	expected := []string{"null", "null"}
	for i, exp := range expected {
		raw, err := dec.NextRaw()
		if err != nil {
			t.Fatalf("item %d: unexpected err: %v", i, err)
		}
		if string(raw) != exp {
			t.Errorf("item %d: expected %s, got %s", i, exp, string(raw))
		}
	}

	_, err := dec.NextRaw()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestStreamDecoder_NextRaw_Mixed(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[1, "hi", true, null, {"id":1,"name":"A"}]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	expected := []string{"1", `"hi"`, "true", "null", `{"id":1,"name":"A"}`}
	for i, exp := range expected {
		raw, err := dec.NextRaw()
		if err != nil {
			t.Fatalf("item %d: unexpected err: %v", i, err)
		}
		if string(raw) != exp {
			t.Errorf("item %d: expected %s, got %s", i, exp, string(raw))
		}
	}

	_, err := dec.NextRaw()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestStreamDecoder_NextRaw_NestedArrays(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[[1,2],[3,4]]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	expected := []string{"[1,2]", "[3,4]"}
	for i, exp := range expected {
		raw, err := dec.NextRaw()
		if err != nil {
			t.Fatalf("item %d: unexpected err: %v", i, err)
		}
		if string(raw) != exp {
			t.Errorf("item %d: expected %s, got %s", i, exp, string(raw))
		}
	}

	_, err := dec.NextRaw()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestStreamDecoder_NextRaw_EscapedStrings(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`["he\"llo","wor\\ld"]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	expected := []string{`"he\"llo"`, `"wor\\ld"`}
	for i, exp := range expected {
		raw, err := dec.NextRaw()
		if err != nil {
			t.Fatalf("item %d: unexpected err: %v", i, err)
		}
		if string(raw) != exp {
			t.Errorf("item %d: expected %s, got %s", i, exp, string(raw))
		}
	}

	_, err := dec.NextRaw()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestStreamDecoder_NextRaw_EmptyArray(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	_, err := dec.NextRaw()
	if err != io.EOF {
		t.Errorf("expected EOF for empty array, got %v", err)
	}
}

func TestStreamDecoder_NextRawBlock_Strings(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`["aaa","bbb","ccc"]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	raw, count, err := dec.NextRawBlock(10, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 elements, got %d", count)
	}
	// The raw block should contain all three strings (with commas between them)
	result := string(raw)
	if !contains(result, `"aaa"`) || !contains(result, `"bbb"`) || !contains(result, `"ccc"`) {
		t.Errorf("raw block missing expected content: %s", result)
	}
}

func TestStreamDecoder_NextRawBlock_Objects(t *testing.T) {
	// Verify existing object path still works
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte(`[{"id":1,"name":"A"},{"id":2,"name":"B"}]`)

	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)

	raw, count, err := dec.NextRawBlock(10, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 objects, got %d", count)
	}
	result := string(raw)
	if !contains(result, `"id":1`) || !contains(result, `"id":2`) {
		t.Errorf("raw block missing expected content: %s", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
