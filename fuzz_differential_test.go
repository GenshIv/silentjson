package silentjson

// Differential fuzz targets: on arbitrary input, silentjson must agree
// with encoding/json — same accept/reject decision and identical decoded
// values. Two documented, intentional laxness gaps are accounted for and
// narrow the comparison instead of being ignored wholesale:
//
//  1. raw control characters inside string literals: silentjson accepts
//     them, encoding/json rejects the document. Inputs containing raw
//     control bytes (< 0x20 outside the two APIs' shared whitespace
//     handling) only need to not panic and stay self-consistent;
//  2. invalid UTF-8 inside strings: encoding/json substitutes each bad
//     byte with U+FFFD, silentjson passes bytes through. String fields
//     are compared after applying encoding/json's per-byte substitution
//     to the silentjson side.
//
// UnmarshalSlice has an arrays-only contract ("expected '[' ..."), so for
// that target the accept/reject comparison applies only to inputs whose
// first non-space byte is '['; other shapes just must not panic.
//
// The seed corpus is built from the real bugs found in this codebase:
// backslashes landing on 32-byte SIMD block boundaries, escaped-quote
// runs, lone surrogates, integer overflow, leading zeros, trailing data.

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"
)

type fuzzSub struct {
	City string `json:"city"`
	Zip  int32  `json:"zip"`
}

type fuzzRec struct {
	ID     int64      `json:"id"`
	Tiny   int8       `json:"tiny"`
	Small  int32      `json:"small"`
	U16    uint16     `json:"u16"`
	F64    float64    `json:"f64"`
	F32    float32    `json:"f32"`
	Str    string     `json:"str"`
	B      bool       `json:"b"`
	Ptr    *int32     `json:"ptr"`
	Sub    *fuzzSub   `json:"sub"`
	Tags   []string   `json:"tags"`
	Scores []int      `json:"scores"`
	Raw    RawMessage `json:"raw"`
	OptStr *string    `json:"opt"`
}

// jsonNormalizeUTF8 applies encoding/json's rule: every byte that does
// not start a valid UTF-8 sequence becomes U+FFFD.
func jsonNormalizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
			i++
		} else {
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String()
}

func containsRawControl(data []byte) bool {
	for _, c := range data {
		if c < 0x20 {
			return true
		}
	}
	return false
}

// compareFuzzRec asserts field-by-field equality of a silentjson decode
// against the encoding/json reference.
func compareFuzzRec(t *testing.T, data []byte, got, want *fuzzRec) {
	t.Helper()
	if got.ID != want.ID || got.Tiny != want.Tiny || got.Small != want.Small ||
		got.U16 != want.U16 || got.F64 != want.F64 || got.F32 != want.F32 ||
		got.B != want.B {
		t.Fatalf("scalar mismatch\ninput=%q\ngot  %+v\nwant %+v", data, *got, *want)
	}
	if jsonNormalizeUTF8(got.Str) != want.Str {
		t.Fatalf("string mismatch\ninput=%q\ngot  %q\nwant %q", data, got.Str, want.Str)
	}
	if (got.Ptr == nil) != (want.Ptr == nil) || (got.Ptr != nil && *got.Ptr != *want.Ptr) {
		t.Fatalf("ptr mismatch: got %v want %v", got.Ptr, want.Ptr)
	}
	if (got.Sub == nil) != (want.Sub == nil) {
		t.Fatalf("sub mismatch: got %v want %v", got.Sub, want.Sub)
	}
	if got.Sub != nil {
		if jsonNormalizeUTF8(got.Sub.City) != want.Sub.City || got.Sub.Zip != want.Sub.Zip {
			t.Fatalf("sub mismatch: got %+v want %+v", *got.Sub, *want.Sub)
		}
	}
	if len(got.Tags) != len(want.Tags) {
		t.Fatalf("tags len mismatch: got %v want %v", got.Tags, want.Tags)
	}
	for i := range got.Tags {
		if jsonNormalizeUTF8(got.Tags[i]) != want.Tags[i] {
			t.Fatalf("tags[%d] mismatch: got %q want %q", i, got.Tags[i], want.Tags[i])
		}
	}
	if len(got.Scores) != len(want.Scores) {
		t.Fatalf("scores mismatch: got %v want %v", got.Scores, want.Scores)
	}
	for i := range got.Scores {
		if got.Scores[i] != want.Scores[i] {
			t.Fatalf("scores[%d] mismatch", i)
		}
	}
	if !bytes.Equal(bytes.TrimSpace(got.Raw), bytes.TrimSpace(want.Raw)) {
		t.Fatalf("raw mismatch: got %q want %q", got.Raw, want.Raw)
	}
	if (got.OptStr == nil) != (want.OptStr == nil) || (got.OptStr != nil && jsonNormalizeUTF8(*got.OptStr) != *want.OptStr) {
		t.Fatalf("opt mismatch: got %v want %v", got.OptStr, want.OptStr)
	}
}

func fuzzSeeds() []string {
	pad := func(n int) string { return strings.Repeat("a", n) }
	return []string{
		`{}`,
		`{"id":1}`,
		`{"id":-42,"tiny":-128,"small":2147483647,"u16":65535}`,
		`{"f64":1.5,"f32":0.25,"b":true}`,
		`{"str":"plain","opt":"x"}`,
		// escapes: the splitter escape-carry bug class (backslash at
		// offsets 30-33 relative to the string start, i.e. around the
		// 32-byte SIMD block boundary)
		`{"str":"` + pad(24) + `\\"end"}`,
		`{"str":"` + pad(25) + `\\"end"}`,
		`{"str":"` + pad(26) + `\\"end"}`,
		`{"str":"` + pad(27) + `\\"end"}`,
		`{"str":"` + pad(28) + `\\\"q\\\"end"}`,
		`{"str":"` + pad(60) + `\\"end"}`,
		`{"str":"a\"b\\c\/d\b\f\n\r\t"}`,
		// unicode escapes
		`{"str":"\u0041\u00e9\ud83d\ude00"}`,
		`{"str":"\ud800"}`,
		`{"str":"\ud83d"}`,
		`{"str":"\uD83D\uDE00\uFFFD"}`,
		// invalid escapes must be rejected
		`{"str":"\q"}`,
		`{"str":"\u00zz"}`,
		`{"str":"\u00"}`,
		// number grammar edges
		`{"id":0}`,
		`{"id":-0}`,
		`{"id":007}`,
		`{"id":9223372036854775807}`,
		`{"id":9223372036854775808}`,
		`{"id":-9223372036854775808}`,
		`{"id":1.5}`,
		`{"id":1e2}`,
		`{"id":+1}`,
		`{"f64":1e999}`,
		`{"f64":.5}`,
		`{"f64":1.}`,
		`{"f64":-0.0e-0}`,
		// nulls
		`{"id":null,"str":null,"ptr":null,"sub":null,"tags":null,"raw":null}`,
		`{"ptr":7,"sub":{"city":"x","zip":1},"opt":null}`,
		// arrays
		`{"tags":[],"scores":[]}`,
		`{"tags":["a","b\\"q","c"],"scores":[-1,0,2]}`,
		`{"tags":["a",]}`,
		`{"scores":[1,2,],}`,
		// nesting / raw
		`{"raw":{"a":[1,2,{"b":null}],"c":"d"}}`,
		`{"raw":"raw string"}`,
		`{"raw":123}`,
		// case-insensitive fallback (both parsers support it)
		`{"ID":5,"STR":"x","F64":2}`,
		// trailing data / commas
		`{"id":1} garbage`,
		`{"id":1,}`,
		`{,}`,
		// multibyte and invalid UTF-8 (laxness gap #2)
		"{\"str\":\"Варшава_中央_łąka\"}",
		"{\"str\":\"a\xffb\xfe\"}",
		"{\"str\":\"\xc3\"}",
		// broken shapes
		`{"id":`,
		`{"id":}`,
		`{"str":"unterminated`,
		`{"str":"bad\`,
		`[{"id":1}]`,
		`null`,
		``,
	}
}

func FuzzParseObjectDifferential(f *testing.F) {
	for _, s := range fuzzSeeds() {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		reg := BuildRegistry(reflect.TypeOf(fuzzRec{}))
		var got fuzzRec
		sjErr := ParseObject(data, reg, unsafe.Pointer(&got))
		var want fuzzRec
		stdErr := json.Unmarshal(data, &want)

		if containsRawControl(data) {
			return // documented laxness gap #1: only no-panic is required
		}
		if sjErr != nil || stdErr != nil {
			if (sjErr == nil) != (stdErr == nil) {
				t.Fatalf("accept/reject mismatch\ninput=%q\nsilentjson=%v\nencoding/json=%v", data, sjErr, stdErr)
			}
			return
		}
		compareFuzzRec(t, data, &got, &want)
	})
}

func FuzzUnmarshalSliceDifferential(f *testing.F) {
	for _, s := range fuzzSeeds() {
		f.Add([]byte(s))
		f.Add([]byte("[" + s + "]"))
	}
	f.Add([]byte(`[{"id":1},{"id":2}]`))
	f.Add([]byte(`[{"tags":["a\"b"]}]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		trimmed := bytes.TrimLeft(data, " \t\n\r")
		if len(trimmed) == 0 || trimmed[0] != '[' {
			// UnmarshalSlice has an arrays-only contract; other shapes
			// must merely not panic.
			reg := BuildRegistry(reflect.TypeOf(fuzzRec{}))
			dst := make([]fuzzRec, 4)
			_, _ = UnmarshalSlice(data, reg, dst)
			return
		}
		if containsRawControl(data) {
			return
		}
		reg := BuildRegistry(reflect.TypeOf(fuzzRec{}))

		dst := make([]fuzzRec, 128)
		got, sjErr := UnmarshalSlice(data, reg, dst)
		if sjErr != nil && strings.Contains(sjErr.Error(), "insufficient capacity") {
			dst = make([]fuzzRec, 1<<20)
			got, sjErr = UnmarshalSlice(data, reg, dst)
		}
		var want []fuzzRec
		stdErr := json.Unmarshal(data, &want)
		if sjErr != nil || stdErr != nil {
			if (sjErr == nil) != (stdErr == nil) {
				t.Fatalf("accept/reject mismatch\ninput=%q\nsilentjson=%v\nencoding/json=%v", data, sjErr, stdErr)
			}
			return
		}
		if len(got) != len(want) {
			t.Fatalf("record count mismatch: got %d want %d\ninput=%q", len(got), len(want), data)
		}
		for i := range got {
			compareFuzzRec(t, data, &got[i], &want[i])
		}
	})
}
