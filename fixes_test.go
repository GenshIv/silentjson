package silentjson

// fixes_test.go — verification tests for the audit patch (P0/P1/P2 fixes).
// Each test maps to a finding from the silentjson audit.

import (
	"bytes"
	"reflect"
	"sync"
	"testing"
	"unsafe"
)

// ---------- P0-1: float32 width ----------

type fixFloat struct {
	A float32 `json:"a"`
	B int32   `json:"b"`
}

func TestFixFloat32ParseNoCorruption(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixFloat{}))
	v := fixFloat{A: 9.99, B: 7}
	if err := ParseObject([]byte(`{"a":1.5,"b":7}`), reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.A != 1.5 {
		t.Errorf("A = %v, want 1.5", v.A)
	}
	if v.B != 7 {
		t.Errorf("B = %v, want 7 (neighbor corrupted?)", v.B)
	}
}

func TestFixFloat32Marshal(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixFloat{}))
	v := fixFloat{A: 1.5, B: 7}
	buf := Marshal(&v, reg, nil)
	if string(buf) != `{"a":1.5,"b":7}` {
		t.Errorf("marshal = %s, want {\"a\":1.5,\"b\":7}", buf)
	}

	type only32 struct {
		A float32 `json:"a"`
	}
	reg2 := BuildRegistry(reflect.TypeOf(only32{}))
	f := only32{A: 1.5}
	if got := string(Marshal(&f, reg2, nil)); got != `{"a":1.5}` {
		t.Errorf("marshal float32-only = %s, want {\"a\":1.5}", got)
	}
}

func TestFixFloat32OmitEmpty(t *testing.T) {
	type om struct {
		A float32 `json:"a,omitempty"`
		B int32   `json:"b"`
	}
	reg := BuildRegistry(reflect.TypeOf(om{}))
	v := om{A: 0, B: 7}
	buf := Marshal(&v, reg, nil)
	if string(buf) != `{"b":7}` {
		t.Errorf("omitempty float32 = %s, want {\"b\":7}", buf)
	}
	v2 := om{A: 1.5, B: 7}
	if got := string(Marshal(&v2, reg, nil)); got != `{"a":1.5,"b":7}` {
		t.Errorf("omitempty float32 non-zero = %s", got)
	}
}

func TestFixFloat32Ptr(t *testing.T) {
	type pf struct {
		A *float32 `json:"a"`
		B *float64 `json:"b"`
	}
	reg := BuildRegistry(reflect.TypeOf(pf{}))
	v := pf{}
	if err := ParseObject([]byte(`{"a":1.5,"b":2.25}`), reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.A == nil || *v.A != 1.5 {
		t.Errorf("*float32 = %v, want 1.5", v.A)
	}
	if v.B == nil || *v.B != 2.25 {
		t.Errorf("*float64 = %v, want 2.25", v.B)
	}
	buf := Marshal(&v, reg, nil)
	if string(buf) != `{"a":1.5,"b":2.25}` {
		t.Errorf("marshal ptrs = %s", buf)
	}
	// null -> nil
	v3 := pf{}
	if err := ParseObject([]byte(`{"a":null,"b":null}`), reg, unsafe.Pointer(&v3)); err != nil {
		t.Fatalf("parse null: %v", err)
	}
	if v3.A != nil || v3.B != nil {
		t.Errorf("null ptrs: %v %v", v3.A, v3.B)
	}
}

// ---------- P0-2: fixed-width ints supported, exotic kinds rejected ----------

type fixWidths struct {
	I8  int8   `json:"i8"`
	I16 int16  `json:"i16"`
	I32 int32  `json:"i32"`
	I64 int64  `json:"i64"`
	U8  uint8  `json:"u8"`
	U32 uint32 `json:"u32"`
}

func TestFixFixedWidthInts(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixWidths{}))
	v := fixWidths{}
	raw := `{"i8":-8,"i16":-1600,"i32":-2000000000,"i64":-9007199254740993,"u8":200,"u32":4000000000}`
	if err := ParseObject([]byte(raw), reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.I8 != -8 || v.I16 != -1600 || v.I32 != -2000000000 || v.I64 != -9007199254740993 || v.U8 != 200 || v.U32 != 4000000000 {
		t.Errorf("widths wrong: %+v", v)
	}
	buf := Marshal(&v, reg, nil)
	v2 := fixWidths{}
	if err := ParseObject(buf, reg, unsafe.Pointer(&v2)); err != nil {
		t.Fatalf("reparse %s: %v", buf, err)
	}
	if v2 != v {
		t.Errorf("roundtrip: %+v != %+v", v2, v)
	}
}

func TestFixOutOfRangeInt(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixWidths{}))
	v := fixWidths{}
	if err := ParseObject([]byte(`{"i32":3000000000}`), reg, unsafe.Pointer(&v)); err == nil {
		t.Error("3000000000 into int32 must fail")
	}
	if err := ParseObject([]byte(`{"u8":256}`), reg, unsafe.Pointer(&v)); err == nil {
		t.Error("256 into uint8 must fail")
	}
	if err := ParseObject([]byte(`{"i8":-129}`), reg, unsafe.Pointer(&v)); err == nil {
		t.Error("-129 into int8 must fail")
	}
}

type fixUnsupported struct {
	V interface{} `json:"v"`
}

func TestFixUnsupportedTypeRejected(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixUnsupported{}))
	if reg.Err() == nil {
		t.Fatal("interface{} field must produce a build error")
	}
	v := fixUnsupported{}
	if err := ParseObject([]byte(`{"v":5}`), reg, unsafe.Pointer(&v)); err == nil {
		t.Error("parse must reject registry with unsupported fields")
	}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("marshal must panic loudly on registry with unsupported fields")
			}
		}()
		_ = Marshal(&v, reg, nil)
	}()
}

type fixUint64Field struct {
	U uint64 `json:"u"`
}

func TestFixUint64Rejected(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixUint64Field{}))
	if reg.Err() == nil {
		t.Fatal("uint64 field must produce a build error")
	}
}

// ---------- P1-1: strict number grammar ----------

type fixNums struct {
	I int     `json:"i"`
	F float64 `json:"f"`
}

func TestFixIntGrammar(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixNums{}))
	cases := []struct {
		raw  string
		want int
		err  bool
	}{
		{`{"i":-42}`, -42, false},
		{`{"i":0}`, 0, false},
		{`{"i":-0}`, 0, false},
		{`{"i":null}`, 0, false},
		{`{"i":3.9}`, 0, true},
		{`{"i":1e2}`, 0, true},
		{`{"i":true}`, 0, true},
		{`{"i":007}`, 0, true},
		{`{"i":99999999999999999999}`, 0, true},
		{`{"i":-9223372036854775808}`, -9223372036854775808, false},
		{`{"i":9223372036854775807}`, 9223372036854775807, false},
		{`{"i":9223372036854775808}`, 0, true},
		{`{"i":-9223372036854775809}`, 0, true},
		{`{"i":""}`, 0, true},
		{`{"i":"5"}`, 0, true},
	}
	for _, c := range cases {
		v := fixNums{}
		err := ParseObject([]byte(c.raw), reg, unsafe.Pointer(&v))
		if c.err && err == nil {
			t.Errorf("%s: expected error, got i=%d", c.raw, v.I)
		}
		if !c.err {
			if err != nil {
				t.Errorf("%s: unexpected error: %v", c.raw, err)
			}
			if v.I != c.want {
				t.Errorf("%s: i = %d, want %d", c.raw, v.I, c.want)
			}
		}
	}
}

func TestFixFloatGrammar(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixNums{}))
	cases := []struct {
		raw  string
		want float64
		err  bool
	}{
		{`{"f":-2.5e1}`, -25, false},
		{`{"f":3.14}`, 3.14, false},
		{`{"f":1e-7}`, 1e-7, false},
		{`{"f":1E+21}`, 1e21, false},
		{`{"f":null}`, 0, false},
		{`{"f":garbage}`, 0, true},
		{`{"f":1e400}`, 0, true}, // out of range
		{`{"f":1.}`, 0, true},
		{`{"f":.5}`, 0, true},
		{`{"f":01.5}`, 0, true},
	}
	for _, c := range cases {
		v := fixNums{}
		err := ParseObject([]byte(c.raw), reg, unsafe.Pointer(&v))
		if c.err && err == nil {
			t.Errorf("%s: expected error, got f=%v", c.raw, v.F)
		}
		if !c.err && (err != nil || v.F != c.want) {
			t.Errorf("%s: f = %v err = %v, want %v", c.raw, v.F, err, c.want)
		}
	}
}

func TestFixIntSliceNegatives(t *testing.T) {
	type sl struct {
		Arr []int `json:"arr"`
	}
	reg := BuildRegistry(reflect.TypeOf(sl{}))
	v := sl{}
	if err := ParseObject([]byte(`{"arr":[-1,2]}`), reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(v.Arr, []int{-1, 2}) {
		t.Errorf("arr = %v, want [-1 2]", v.Arr)
	}
	v2 := sl{}
	if err := ParseObject([]byte(`{"arr":[1,]}`), reg, unsafe.Pointer(&v2)); err == nil {
		t.Error("trailing comma in int array must fail")
	}
}

// ---------- P1-2: escape sequences ----------

type fixStr struct {
	S string `json:"s"`
}

// uEsc builds a JSON escape sequence like "\uXXXX" without a literal
// backslash-u pair in this source file.
func uEsc(hex string) string { return `\` + `u` + hex }

func TestFixUnicodeEscapes(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixStr{}))
	tbl := []struct {
		raw  string
		want string
		err  bool
	}{
		{`{"s":"` + uEsc(`0041`) + uEsc(`0042`) + `"}`, "AB", false},
		{`{"s":"` + uEsc(`d83d`) + uEsc(`de00`) + `"}`, "\U0001F600", false},
		{`{"s":"a` + uEsc(`0007`) + `b"}`, "a\x07b", false},
		{`{"s":"a\nb"}`, "a\nb", false},
		{`{"s":"a\/b"}`, "a/b", false},
		{`{"s":"` + uEsc(`d83d`) + `"}`, "\uFFFD", false}, // lone surrogate -> U+FFFD
		{`{"s":"` + uEsc(`00zz`) + `"}`, "", true},        // invalid hex
		{`{"s":"a\qb"}`, "", true},                        // invalid escape char
		{`{"s":""}`, "", false},                           // empty string
		{`{"s":"кириллица"}`, "кириллица", false},         // raw UTF-8 passthrough
	}
	for _, c := range tbl {
		v := fixStr{}
		err := ParseObject([]byte(c.raw), reg, unsafe.Pointer(&v))
		if c.err {
			if err == nil {
				t.Errorf("%s: expected error, got %q", c.raw, v.S)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.raw, err)
			continue
		}
		if v.S != c.want {
			t.Errorf("%s: s = %q, want %q", c.raw, v.S, c.want)
		}
	}
}

// ---------- P1-3: buffer ownership (no in-place mutation) ----------

func TestFixNoInputMutation(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixStr{}))

	// escaped string: buffer must stay byte-identical
	escapedJSON := `{"s":"a\\b` + uEsc(`0041`) + `c"}`
	raw := []byte(escapedJSON)
	orig := string(raw)
	v := fixStr{}
	if err := ParseObject(raw, reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if string(raw) != orig {
		t.Errorf("input buffer mutated: %q -> %q", orig, string(raw))
	}
	if v.S != "a\\bAc" {
		t.Errorf("escaped decode = %q, want a\\bAc", v.S)
	}

	// clean string: still zero-copy aliased (documented behavior)
	raw2 := []byte(`{"s":"hello"}`)
	v2 := fixStr{}
	if err := ParseObject(raw2, reg, unsafe.Pointer(&v2)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	copy(raw2, []byte(`{"s":"HACKE"}`))
	if v2.S != "HACKE" {
		t.Errorf("clean strings must stay aliased to the input buffer, got %q", v2.S)
	}

	// escaped strings must NOT alias the input
	raw3 := []byte(`{"s":"a\\b"}`)
	v3 := fixStr{}
	if err := ParseObject(raw3, reg, unsafe.Pointer(&v3)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	copy(raw3, []byte(`{"s":"XXXXX"}`))
	if v3.S != "a\\b" {
		t.Errorf("escaped strings must be independent copies, got %q", v3.S)
	}
}

func TestFixCopyStringsRegistry(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixStr{}))
	reg.CopyStrings = true
	v := fixStr{}
	if err := ParseObject([]byte(`{"s":"hello"}`), reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.S != "hello" {
		t.Errorf("s = %q", v.S)
	}
}

// ---------- P1-4: concurrent BuildRegistry / registry safety ----------

type fixRecursive struct {
	Val  int            `json:"val"`
	Next *fixRecursive  `json:"next"`
	Subs []fixRecursive `json:"subs"`
}

type sjTestErr string

func (e sjTestErr) Error() string { return string(e) }

func TestFixBuildRegistryConcurrent(t *testing.T) {
	typ := reflect.TypeOf(fixRecursive{})
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg := BuildRegistry(typ)
			if reg == nil {
				errs <- sjTestErr("nil registry")
				return
			}
			// use it right away: must not race with other builders
			v := fixRecursive{}
			if err := ParseObject([]byte(`{"val":1,"next":{"val":2}}`), reg, unsafe.Pointer(&v)); err != nil {
				errs <- sjTestErr(err.Error())
				return
			}
			if v.Val != 1 || v.Next == nil || v.Next.Val != 2 {
				errs <- sjTestErr("bad parse result")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

func TestFixBuildRegistryRecursive(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixRecursive{}))
	v := fixRecursive{}
	raw := `{"val":1,"next":{"val":2,"next":{"val":3}},"subs":[{"val":4},{"val":5}]}`
	if err := ParseObject([]byte(raw), reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Val != 1 || v.Next.Val != 2 || v.Next.Next.Val != 3 || len(v.Subs) != 2 || v.Subs[1].Val != 5 {
		t.Errorf("recursive parse wrong: %+v", v)
	}
}

// ---------- P2-1: trailing data / trailing commas ----------

type fixTail struct {
	I int `json:"i"`
}

func TestFixTrailingData(t *testing.T) {
	regArr := BuildRegistry(reflect.TypeOf([]fixTail{}))
	slc := make([]fixTail, 4)
	if err := ParseObject([]byte(`[{"i":1}]GARBAGE`), regArr, unsafe.Pointer(&slc)); err == nil {
		t.Error("trailing garbage after root array must fail")
	}
	slc2 := make([]fixTail, 4)
	if err := ParseObject([]byte(`[{"i":1}] [{"i":2}]`), regArr, unsafe.Pointer(&slc2)); err == nil {
		t.Error("second root value must be reported as trailing data")
	}

	regObj := BuildRegistry(reflect.TypeOf(fixTail{}))
	vt := fixTail{}
	if err := ParseObject([]byte(`{"i":1,}`), regObj, unsafe.Pointer(&vt)); err == nil {
		t.Error("trailing comma in object must fail")
	}
	if err := ParseObject([]byte(`{"i":1}`), regObj, unsafe.Pointer(&vt)); err != nil {
		t.Errorf("valid object must parse: %v", err)
	}
	if err := ParseObject([]byte(`{}`), regObj, unsafe.Pointer(&vt)); err != nil {
		t.Errorf("empty object must parse: %v", err)
	}
	if err := ParseObject([]byte(`{,}`), regObj, unsafe.Pointer(&vt)); err == nil {
		t.Error("{,} must fail")
	}
	if err := ParseObject([]byte(`  {"i":1}  `), regObj, unsafe.Pointer(&vt)); err != nil {
		t.Errorf("trailing whitespace is fine: %v", err)
	}
	slc3 := make([]fixTail, 4)
	if err := ParseObject([]byte(`[]`), regArr, unsafe.Pointer(&slc3)); err != nil {
		t.Errorf("empty root array must parse: %v", err)
	}
}

// ---------- P2-2: encoding/json compat bits ----------

func TestFixPtrStructParse(t *testing.T) {
	type sub struct {
		X int `json:"x"`
	}
	type root struct {
		I int  `json:"i"`
		P *sub `json:"p"`
	}
	reg := BuildRegistry(reflect.TypeOf(root{}))
	v := root{}
	if err := ParseObject([]byte(`{"i":1,"p":{"x":9}}`), reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.P == nil || v.P.X != 9 {
		t.Errorf("P = %+v, want x=9", v.P)
	}
	buf := Marshal(&v, reg, nil)
	if string(buf) != `{"i":1,"p":{"x":9}}` {
		t.Errorf("marshal = %s", buf)
	}
	// null -> nil
	v2 := root{}
	if err := ParseObject([]byte(`{"i":1,"p":null}`), reg, unsafe.Pointer(&v2)); err != nil {
		t.Fatalf("parse null: %v", err)
	}
	if v2.P != nil {
		t.Errorf("P must stay nil after null, got %+v", v2.P)
	}
	if got := string(Marshal(&v2, reg, nil)); got != `{"i":1,"p":null}` {
		t.Errorf("marshal nil ptr = %s", got)
	}
}

func TestFixRawMessageParse(t *testing.T) {
	type root struct {
		R RawMessage `json:"r"`
	}
	reg := BuildRegistry(reflect.TypeOf(root{}))
	v := root{}
	if err := ParseObject([]byte(`{"r":{"x":[1,2]},"after":true}`), reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if string(v.R) != `{"x":[1,2]}` {
		t.Errorf("R = %s, want {\"x\":[1,2]}", v.R)
	}
	buf := Marshal(&v, reg, nil)
	if string(buf) != `{"r":{"x":[1,2]}}` {
		t.Errorf("marshal = %s", buf)
	}
	// copy semantics: mutating the input must not affect stored RawMessage
	v2 := root{}
	raw := []byte(`{"r":{"x":1}}`)
	if err := ParseObject(raw, reg, unsafe.Pointer(&v2)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	copy(raw, []byte(`{"r":{"x":9}}`))
	if string(v2.R) != `{"x":1}` {
		t.Errorf("RawMessage must be a copy, got %s", v2.R)
	}
}

func TestFixCaseInsensitiveKeys(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixStr{}))
	v := fixStr{}
	if err := ParseObject([]byte(`{"S":"x"}`), reg, unsafe.Pointer(&v)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.S != "x" {
		t.Errorf("case-insensitive fallback failed: %q", v.S)
	}
	// exact match still works
	v2 := fixStr{}
	if err := ParseObject([]byte(`{"s":"y"}`), reg, unsafe.Pointer(&v2)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v2.S != "y" {
		t.Errorf("exact key match broken: %q", v2.S)
	}
}

// ---------- stream ----------

func TestFixStreamNoRegistryMutation(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixStr{}))
	before := reg.CopyStrings
	stream := `[{"s":"` + uEsc(`0041`) + `B"},{"s":"plain"}]`
	dec := NewStreamDecoder[fixStr](bytes.NewReader([]byte(stream)), reg)
	var got []string
	if err := dec.Next(func(obj *fixStr) bool {
		got = append(got, obj.S)
		return true
	}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if reg.CopyStrings != before {
		t.Errorf("NewStreamDecoder must not mutate the shared registry")
	}
	if len(got) != 2 || got[0] != "AB" || got[1] != "plain" {
		t.Errorf("stream items = %v, want [AB plain] (escape must decode)", got)
	}
}

// ---------- raw.go utils ----------

func TestFixGetStringValue(t *testing.T) {
	// top level wins over nested
	data := []byte(`{"nested":{"id":"WRONG"},"id":"RIGHT"}`)
	if v, ok := GetStringValue(data, "id"); !ok || v != "RIGHT" {
		t.Errorf("GetStringValue = %q ok=%v, want RIGHT", v, ok)
	}
	// nested fallback
	data2 := []byte(`{"a":1,"nested":{"id":"RIGHT"}}`)
	if v, ok := GetStringValue(data2, "id"); !ok || v != "RIGHT" {
		t.Errorf("nested GetStringValue = %q ok=%v", v, ok)
	}
	// no false positives from string values
	data3 := []byte(`{"msg":"see \"id\": here","other":"x"}`)
	if _, ok := GetStringValue(data3, "id"); ok {
		t.Error("GetStringValue must not match inside string values")
	}
	// escaped key match
	data4 := []byte(`{"a\tb":"val"}`)
	if v, ok := GetStringValue(data4, "a\tb"); !ok || v != "val" {
		t.Errorf("escaped key = %q ok=%v", v, ok)
	}
	// missing
	if _, ok := GetStringValue([]byte(`{"a":1}`), "zz"); ok {
		t.Error("missing key must return ok=false")
	}
}

func TestFixGetInt64Value(t *testing.T) {
	if v, ok := GetInt64Value([]byte(`{"id":-5}`), "id"); !ok || v != -5 {
		t.Errorf("GetInt64Value(-5) = %d ok=%v", v, ok)
	}
	if _, ok := GetInt64Value([]byte(`{"id":3.14}`), "id"); ok {
		t.Error("float must not match GetInt64Value")
	}
	if _, ok := GetInt64Value([]byte(`{"id":"x"}`), "id"); ok {
		t.Error("string must not match GetInt64Value")
	}
	if v, ok := GetInt64Value([]byte(`{"deep":{"n":42}}`), "n"); !ok || v != 42 {
		t.Errorf("nested int = %d ok=%v", v, ok)
	}
}

func TestFixInjectFieldBeforeClose(t *testing.T) {
	// regular object
	out := InjectFieldBeforeClose([]byte(`{"a":1}`), []byte("k"), []byte("v"))
	if string(out) != `{"a":1,"k":"v"}` {
		t.Errorf("inject = %s", out)
	}
	// empty object: no leading comma
	out2 := InjectFieldBeforeClose([]byte(`{}`), []byte("k"), []byte("v"))
	if string(out2) != `{"k":"v"}` {
		t.Errorf("inject empty object = %s", out2)
	}
	// invalid inputs returned unchanged (no panic)
	if got := InjectFieldBeforeClose(nil, []byte("k"), []byte("v")); got != nil {
		t.Errorf("nil input must be returned unchanged, got %s", got)
	}
	if got := InjectFieldBeforeClose([]byte(`"str"`), []byte("k"), []byte("v")); string(got) != `"str"` {
		t.Errorf("non-object input must be returned unchanged, got %s", got)
	}
	// same for InjectFieldBeforeClose1
	out3 := InjectFieldBeforeClose1([]byte(`{}`), []byte("k"), []byte("v"))
	if string(out3) != `{"k":"v"}` {
		t.Errorf("inject1 empty object = %s", out3)
	}
	if got := InjectFieldBeforeClose1([]byte(`"str"`), []byte("k"), []byte("v")); string(got) != `"str"` {
		t.Errorf("inject1 non-object = %s", got)
	}
}

// ---------- regression: composite roundtrip ----------

type fixDeep struct {
	Name   string     `json:"name"`
	Count  int32      `json:"count"`
	Ratio  float32    `json:"ratio"`
	Active bool       `json:"active"`
	Tags   []string   `json:"tags"`
	Nums   []int      `json:"nums"`
	Raw    RawMessage `json:"raw"`
	Ptr    *fixTail   `json:"ptr"`
}

func TestFixCompositeRoundtrip(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(fixDeep{}))
	src := fixDeep{
		Name:   "тест ✓",
		Count:  -42,
		Ratio:  2.5,
		Active: true,
		Tags:   []string{"a", "b\n", "c\"d"},
		Nums:   []int{-1, 2, 3},
		Raw:    RawMessage(`{"k":[1]}`),
		Ptr:    &fixTail{I: 7},
	}
	buf := Marshal(&src, reg, nil)
	dst := fixDeep{}
	if err := ParseObject(buf, reg, unsafe.Pointer(&dst)); err != nil {
		t.Fatalf("parse %s: %v", buf, err)
	}
	if !reflect.DeepEqual(dst, src) {
		t.Errorf("roundtrip mismatch:\n got %+v\nwant %+v\njson: %s", dst, src, buf)
	}
}

func TestFixOmitEmptyWidths(t *testing.T) {
	type om struct {
		I8 int8  `json:"i8,omitempty"`
		I3 int32 `json:"i3,omitempty"`
		U8 uint8 `json:"u8,omitempty"`
	}
	reg := BuildRegistry(reflect.TypeOf(om{}))
	v := om{}
	if got := string(Marshal(&v, reg, nil)); got != `{}` {
		t.Errorf("all-zero omitempty = %s, want {}", got)
	}
	v2 := om{I8: 1, U8: 2}
	if got := string(Marshal(&v2, reg, nil)); got != `{"i8":1,"u8":2}` {
		t.Errorf("non-zero omitempty = %s", got)
	}
}
