package silentjson

// float_truncation_test.go — P1-B pins: a number token cut off by the end
// of the input is an unexpected EOF, not a type mismatch. encoding/json
// classifies exactly these shapes at the buffer end as "unexpected end of
// JSON input", and the strict decoder must do the same WITHOUT throwing
// away the value parsed before the cut (the same contract as the stream
// tail: data first, then the sticky error).

import (
	"errors"
	"reflect"
	"testing"
	"unsafe"
)

type truncTarget struct {
	Price float64  `json:"price"`
	Ratio *float64 `json:"ratio"`
	Small float32  `json:"small"`
	Note  string   `json:"note"`
}

// Truncated tokens at the buffer end: ErrUnexpectedEOF, partial value kept.
func TestFloatTruncatedAtEOFReportsEOFAndKeepsPartial(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want float64 // partial value expected in .Price after the failed decode
	}{
		{"fraction without digits", `{"price":12.`, 12},
		{"exponent without digits", `{"price":12.5e`, 12.5},
		{"exponent sign without digits", `{"price":12.5e+`, 12.5},
		{"sign without digits", `{"price":-`, 0},
		{"complete token, envelope cut", `{"price":12.5`, 12.5},
		// the fast path refuses the past-2^53 mantissa before it can see
		// the cut: no exact partial exists, so the value stays 0 — but
		// the error must still be classified as EOF, not a type mismatch
		{"big mantissa, fraction cut", `{"price":9007199254740995.`, 0},
	}
	for _, c := range cases {
		reg := BuildRegistry(reflect.TypeOf(truncTarget{}))
		var got truncTarget
		err := ParseObject([]byte(c.in), reg, unsafe.Pointer(&got))
		if !errors.Is(err, ErrUnexpectedEOF) {
			t.Errorf("%s: ParseObject(%q) error = %v, want ErrUnexpectedEOF", c.name, c.in, err)
			continue
		}
		if got.Price != c.want {
			t.Errorf("%s: Price = %v, want partial %v", c.name, got.Price, c.want)
		}
	}
}

// Fields decoded before the cut keep their values.
func TestFloatTruncatedAtEOFPreservesEarlierFields(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(truncTarget{}))
	var got truncTarget
	err := ParseObject([]byte(`{"note":"kept","small":1.5,"price":12.`), reg, unsafe.Pointer(&got))
	if !errors.Is(err, ErrUnexpectedEOF) {
		t.Fatalf("error = %v, want ErrUnexpectedEOF", err)
	}
	if got.Note != "kept" || got.Small != 1.5 || got.Price != 12 {
		t.Errorf("partial state = {note:%q small:%v price:%v}, want kept/1.5/12", got.Note, got.Small, got.Price)
	}
}

// A *float64 is allocated and filled with the partial value, then the EOF
// is reported — the pointer must not stay nil on a truncation error.
func TestFloatTruncatedAtEOFPointerPartial(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(truncTarget{}))
	var got truncTarget
	if err := ParseObject([]byte(`{"ratio":1.5e`), reg, unsafe.Pointer(&got)); !errors.Is(err, ErrUnexpectedEOF) {
		t.Fatalf("error = %v, want ErrUnexpectedEOF", err)
	}
	if got.Ratio == nil || *got.Ratio != 1.5 {
		t.Errorf("Ratio = %v, want allocated with partial 1.5", got.Ratio)
	}

	var fresh truncTarget
	if err := ParseObject([]byte(`{"ratio":0.`), reg, unsafe.Pointer(&fresh)); !errors.Is(err, ErrUnexpectedEOF) {
		t.Fatalf("error = %v, want ErrUnexpectedEOF", err)
	}
	if fresh.Ratio == nil || *fresh.Ratio != 0 {
		t.Errorf("Ratio = %v, want allocated with partial 0", fresh.Ratio)
	}
}

// Unknown-field values go through skipValueStrict: same classification.
func TestTruncatedNumberUnknownFieldAtEOF(t *testing.T) {
	type wrapper struct {
		Note string `json:"note"`
	}
	reg := BuildRegistry(reflect.TypeOf(wrapper{}))
	var got wrapper
	if err := ParseObject([]byte(`{"other":12.`), reg, unsafe.Pointer(&got)); !errors.Is(err, ErrUnexpectedEOF) {
		t.Fatalf("error = %v, want ErrUnexpectedEOF", err)
	}
}

// A truncated SHAPE followed by a delimiter is a grammar error, not EOF —
// encoding/json reports an invalid character there, and so do we. No
// partial value is written: the grammar broke before the input did.
func TestTruncatedShapeWithDelimiterStaysTypeMismatch(t *testing.T) {
	cases := []struct {
		in        string
		wantPrice float64
	}{
		{`{"price":12.,"note":"x"}`, 0},
		{`{"price":12x}`, 0},
		{`{"price":.5}`, 0},
		{`{"price":01}`, 0},
		{`{"price":12.5.6}`, 0},
		{`{"price":1e}`, 0},
	}
	for _, c := range cases {
		reg := BuildRegistry(reflect.TypeOf(truncTarget{}))
		var got truncTarget
		err := ParseObject([]byte(c.in), reg, unsafe.Pointer(&got))
		if !errors.Is(err, ErrTypeMismatch) {
			t.Errorf("ParseObject(%q) error = %v, want ErrTypeMismatch", c.in, err)
			continue
		}
		if errors.Is(err, ErrUnexpectedEOF) {
			t.Errorf("ParseObject(%q): type mismatch must not wrap ErrUnexpectedEOF", c.in)
		}
		if got.Price != c.wantPrice {
			t.Errorf("ParseObject(%q): Price = %v, want untouched %v", c.in, got.Price, c.wantPrice)
		}
	}
}

// Unit pins on the primitives themselves.
func TestParseJSONFloatTruncationClassification(t *testing.T) {
	truncated := []struct {
		in   string
		want float64
	}{
		{"12.", 12}, {"-12.", -12}, {"12.5e", 12.5}, {"12.5e+", 12.5},
		{"12.5e-", 12.5}, {"-", 0}, {"1.", 1}, {"0.", 0},
		{"9007199254740995.", 0}, // refused by the fast path, caught by incompleteNumber
	}
	for _, c := range truncated {
		v, ok, trunc := parseJSONFloat([]byte(c.in))
		if ok || !trunc {
			t.Errorf("parseJSONFloat(%q) = (%v, ok=%v, trunc=%v), want (partial, false, true)", c.in, v, ok, trunc)
			continue
		}
		if v != c.want {
			t.Errorf("parseJSONFloat(%q) partial = %v, want %v", c.in, v, c.want)
		}
		if !incompleteNumber([]byte(c.in)) {
			t.Errorf("incompleteNumber(%q) = false, want true", c.in)
		}
	}

	complete := []string{"12", "12.5", "1e2", "0", "-0", "1e-999", "9007199254740995"}
	for _, in := range complete {
		_, ok, trunc := parseJSONFloat([]byte(in))
		if !ok || trunc {
			t.Errorf("parseJSONFloat(%q): ok=%v trunc=%v, want accepted as complete", in, ok, trunc)
		}
		if incompleteNumber([]byte(in)) {
			t.Errorf("incompleteNumber(%q) = true, want false (complete)", in)
		}
	}

	garbage := []string{"", ".5", "01", "1x", "12x", "1.2.3", "1ex", "1e2x", "--1", "+1", "12.e2", "1e999"}
	for _, in := range garbage {
		_, ok, trunc := parseJSONFloat([]byte(in))
		if ok || trunc {
			t.Errorf("parseJSONFloat(%q): ok=%v trunc=%v, want plain garbage", in, ok, trunc)
		}
		if incompleteNumber([]byte(in)) {
			t.Errorf("incompleteNumber(%q) = true, want false", in)
		}
	}
}
