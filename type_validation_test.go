package silentjson

import (
	"bytes"
	"io"
	"reflect"
	"testing"
	"time"
	"unsafe"
)

// Two struct types with identical size but different layouts
type TypeA struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type TypeB struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func TestTypeValidation_MatchingTypes(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	jsonStr := `[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`
	dst := make([]TypeA, 2)
	result, err := UnmarshalSlice([]byte(jsonStr), reg, dst)
	if err != nil {
		t.Fatalf("matching types should succeed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestTypeValidation_DifferentTypesSameSize(t *testing.T) {
	// TypeA and TypeB have the same size but different field layouts
	if unsafe.Sizeof(TypeA{}) != unsafe.Sizeof(TypeB{}) {
		t.Skipf("types have different sizes (%d vs %d), test not applicable",
			unsafe.Sizeof(TypeA{}), unsafe.Sizeof(TypeB{}))
	}

	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	jsonStr := `[{"id":1,"name":"Alice"}]`
	dst := make([]TypeB, 1)
	_, err := UnmarshalSlice([]byte(jsonStr), reg, dst)
	if err == nil {
		t.Fatal("different types with same size should fail validation")
	}
	if !typeContains(err.Error(), "registry built for") {
		t.Errorf("expected type mismatch error, got: %v", err)
	}
}

func TestTypeValidation_NilRegistry(t *testing.T) {
	jsonStr := `[{"id":1}]`
	dst := make([]TypeA, 1)
	_, err := UnmarshalSlice([]byte(jsonStr), nil, dst)
	if err == nil {
		t.Fatal("nil registry should fail")
	}
	if !typeContains(err.Error(), "registry is nil") {
		t.Errorf("expected nil registry error, got: %v", err)
	}
}

func TestTypeValidation_UnmarshalSliceTolerant_Mismatch(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	jsonStr := `[{"id":1}]`
	dst := make([]TypeB, 1)
	_, problems := UnmarshalSliceTolerant([]byte(jsonStr), reg, dst)
	if len(problems) == 0 {
		t.Fatal("expected type mismatch problem")
	}
	if !typeContains(problems[0].Err.Error(), "registry built for") {
		t.Errorf("expected type mismatch error, got: %v", problems[0].Err)
	}
}

func TestTypeValidation_UnmarshalArrayParallel_Mismatch(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	jsonStr := `[{"id":1}]`
	dst := make([]TypeB, 1)
	_, err := UnmarshalArrayParallel([]byte(jsonStr), reg, dst)
	if err == nil {
		t.Fatal("expected type mismatch error")
	}
	if !typeContains(err.Error(), "registry built for") {
		t.Errorf("expected type mismatch error, got: %v", err)
	}
}

func TestTypeValidation_StreamDecoder_Matching(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	jsonStr := `[{"id":1,"name":"Alice"}]`
	r := bytes.NewReader([]byte(jsonStr))
	dec := NewStreamDecoder[TypeA](r, reg)

	var obj TypeA
	err := dec.Decode(&obj)
	if err != nil {
		t.Fatalf("matching types should succeed: %v", err)
	}
	if obj.ID != 1 || obj.Name != "Alice" {
		t.Errorf("unexpected values: %+v", obj)
	}
}

func TestTypeValidation_StreamDecoder_Mismatch(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	jsonStr := `[{"id":1,"name":"Alice"}]`
	r := bytes.NewReader([]byte(jsonStr))
	dec := NewStreamDecoder[TypeB](r, reg)

	var obj TypeB
	err := dec.Decode(&obj)
	if err == nil {
		t.Fatal("expected type mismatch error")
	}
	if !typeContains(err.Error(), "registry built for") {
		t.Errorf("expected type mismatch error, got: %v", err)
	}
}

func TestTypeValidation_StreamDecoder_NilRegistry(t *testing.T) {
	r := bytes.NewReader([]byte(`[{"id":1}]`))
	dec := NewStreamDecoder[TypeA](r, nil)

	var obj TypeA
	err := dec.Decode(&obj)
	if err == nil {
		t.Fatal("expected nil registry error")
	}
	if !typeContains(err.Error(), "registry is nil") {
		t.Errorf("expected nil registry error, got: %v", err)
	}
}

func TestTypeValidation_StreamDecoder_Next_Mismatch(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	r := bytes.NewReader([]byte(`[{"id":1}]`))
	dec := NewStreamDecoder[TypeB](r, reg)

	err := dec.Next(func(obj *TypeB) bool { return true })
	if err == nil {
		t.Fatal("expected type mismatch error")
	}
	if !typeContains(err.Error(), "registry built for") {
		t.Errorf("expected type mismatch error, got: %v", err)
	}
}

func TestTypeValidation_StreamDecoder_NextChan_Mismatch(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	r := bytes.NewReader([]byte(`[{"id":1}]`))
	dec := NewStreamDecoder[TypeB](r, reg)

	ch := dec.NextChan(1)
	result := <-ch
	if result.Err == nil {
		t.Fatal("expected type mismatch error")
	}
	if !typeContains(result.Err.Error(), "registry built for") {
		t.Errorf("expected type mismatch error, got: %v", result.Err)
	}
}

func TestTypeValidation_StreamDecoder_NextChanParallel_Mismatch(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	r := bytes.NewReader([]byte(`[{"id":1}]`))
	dec := NewStreamDecoder[TypeB](r, reg)

	ch := dec.NextChanParallel(2, 1)
	result := <-ch
	if result.Err == nil {
		t.Fatal("expected type mismatch error")
	}
	if !typeContains(result.Err.Error(), "registry built for") {
		t.Errorf("expected type mismatch error, got: %v", result.Err)
	}
}

func TestTypeValidation_StreamDecoder_NextRaw_Mismatch(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	r := bytes.NewReader([]byte(`[{"id":1}]`))
	dec := NewStreamDecoder[TypeB](r, reg)

	_, err := dec.NextRaw()
	if err == nil {
		t.Fatal("expected type mismatch error")
	}
	if !typeContains(err.Error(), "registry built for") {
		t.Errorf("expected type mismatch error, got: %v", err)
	}
}

func typeContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && typeContainsAt(s, substr))
}

func typeContainsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Test that all three parallel.go APIs reject a mismatched Registry type
// even when raw is empty. This validates that the type check happens before
// the empty-input early return.
func TestTypeValidation_EmptyInput_Mismatch(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))

	// UnmarshalSlice with empty input and mismatched type
	_, err := UnmarshalSlice[TypeB]([]byte{}, reg, make([]TypeB, 0))
	if err == nil {
		t.Error("UnmarshalSlice should reject mismatched type even with empty input")
	}

	// UnmarshalSliceTolerant with empty input and mismatched type
	_, problems := UnmarshalSliceTolerant[TypeB]([]byte{}, reg, make([]TypeB, 0))
	if len(problems) == 0 {
		t.Error("UnmarshalSliceTolerant should reject mismatched type even with empty input")
	}

	// UnmarshalArrayParallel with empty input and mismatched type
	_, err = UnmarshalArrayParallel[TypeB]([]byte{}, reg, make([]TypeB, 0))
	if err == nil {
		t.Error("UnmarshalArrayParallel should reject mismatched type even with empty input")
	}
}

// Test that matching types with empty input still work correctly.
func TestTypeValidation_EmptyInput_Matching(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))

	result, err := UnmarshalSlice[TypeA]([]byte{}, reg, make([]TypeA, 0))
	if err != nil {
		t.Fatalf("matching types with empty input should succeed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d items", len(result))
	}
}

// Test that NextChanParallel with an initialization error emits exactly one
// error result and closes the channel.
func TestTypeValidation_NextChanParallel_InitError(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))
	r := bytes.NewReader([]byte(`[{"id":1}]`))
	dec := NewStreamDecoder[TypeB](r, reg)

	ch := dec.NextChanParallel(2, 1)

	// Should receive exactly one error result
	result := <-ch
	if result.Err == nil {
		t.Fatal("expected initialization error")
	}
	if !typeContains(result.Err.Error(), "registry built for") {
		t.Errorf("expected type mismatch error, got: %v", result.Err)
	}

	// Channel should be closed after the single error
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after init error")
		}
	case <-time.After(time.Second):
		t.Fatal("channel was not closed")
	}
}

// Test that no parsing or input-reading work is performed when initErr is set.
// We use a reader that signals when Read() is invoked to verify this.
func TestTypeValidation_NextChanParallel_NoWorkOnInitError(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TypeA{}))

	// Use a reader that signals when Read() is called
	readCalled := make(chan struct{}, 1)
	r := &signalingReader{ch: readCalled}
	dec := NewStreamDecoder[TypeB](r, reg)

	ch := dec.NextChanParallel(1, 1)

	// Should receive the init error immediately without invoking the reader
	select {
	case result := <-ch:
		if result.Err == nil {
			t.Fatal("expected initialization error")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked waiting for result; reader was likely called")
	}

	// Verify the reader was never called by checking that readCalled is still empty
	select {
	case <-readCalled:
		t.Error("reader was called despite init error")
	default:
		// Good - reader was not called
	}
}

// signalingReader signals on a channel when Read() is invoked.
type signalingReader struct {
	ch chan struct{}
}

func (r *signalingReader) Read(p []byte) (int, error) {
	select {
	case r.ch <- struct{}{}:
	default:
	}
	return 0, io.EOF
}
