package silentjson

import (
	"encoding/json"
	"reflect"
	"testing"
)

type floatPayload struct {
	Price    float64  `json:"price"`
	MinPrice float64  `json:"min_price"`
	Big      float64  `json:"big,omitempty"`
	Nil      *float64 `json:"nil,omitempty"`
}

// TestMarshalFloatPlainNotation pins the encoding/json-compatible float
// formatting: everyday prices must serialize as plain decimals, not ryu's
// always-exponent form (2.319e+03).
func TestMarshalFloatPlainNotation(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(floatPayload{}))
	p := floatPayload{Price: 2319, MinPrice: 99.99, Big: 1e22}
	got := string(Marshal(&p, reg, nil))
	want := `{"price":2319,"min_price":99.99,"big":1e+22}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	var back floatPayload
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Price != 2319 || back.MinPrice != 99.99 || back.Big != 1e22 {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}
