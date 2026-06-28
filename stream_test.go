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
