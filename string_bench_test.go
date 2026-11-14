package silentjson

import (
	"bytes"
	"reflect"
	"testing"
	"unsafe"
)

// Benchmark string parsing with various escape patterns.
// Each benchmark parses a JSON object with a single string field "s".

func benchString(b *testing.B, json []byte) {
	type T struct {
		S string `json:"s"`
	}
	typ := reflect.TypeOf(T{})
	reg := BuildRegistry(typ)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result T
		ptr := unsafe.Pointer(&result)
		if err := ParseObject(json, reg, ptr); err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

func BenchmarkString_NoEscapes_Short(b *testing.B) {
	json := []byte(`{"s":"hello world"}`)
	benchString(b, json)
}

func BenchmarkString_NoEscapes_Medium(b *testing.B) {
	s := make([]byte, 1024)
	for i := range s {
		s[i] = 'a' + byte(i%26)
	}
	json := []byte(`{"s":"`)
	json = append(json, s...)
	json = append(json, `"}`...)
	benchString(b, json)
}

func BenchmarkString_NoEscapes_Long(b *testing.B) {
	s := make([]byte, 10240)
	for i := range s {
		s[i] = 'a' + byte(i%26)
	}
	json := []byte(`{"s":"`)
	json = append(json, s...)
	json = append(json, `"}`...)
	benchString(b, json)
}

func BenchmarkString_OneEscape_Middle(b *testing.B) {
	s := make([]byte, 512)
	for i := range s {
		s[i] = 'a' + byte(i%26)
	}
	json := []byte(`{"s":"`)
	json = append(json, s...)
	json = append(json, `\n`...)
	json = append(json, s[512:]...)
	json = append(json, `"}`...)
	benchString(b, json)
}

func BenchmarkString_OneEscape_Start(b *testing.B) {
	s := make([]byte, 1024)
	for i := range s {
		s[i] = 'a' + byte(i%26)
	}
	json := []byte(`{"s":"`)
	json = append(json, `\n`...) // JSON escape sequence (two chars: backslash, n)
	json = append(json, s...)
	json = append(json, `"}`...)
	benchString(b, json)
}

func BenchmarkString_FrequentEscapes(b *testing.B) {
	var buf bytes.Buffer
	buf.WriteString(`{"s":"`)
	for i := 0; i < 100; i++ {
		buf.WriteString("text\n")
	}
	buf.WriteString(`"}`)
	benchString(b, buf.Bytes())
}

func BenchmarkString_BackslashSequence(b *testing.B) {
	json := []byte(`{"s":"path\\to\\file\\here"}`)
	benchString(b, json)
}

func BenchmarkString_BackslashBeforeQuote(b *testing.B) {
	// JSON: {"s":"say \"hello\""}
	// The escaped quotes are part of the string content
	json := []byte(`{"s":"say \"hello\""}`)
	benchString(b, json)
}

func BenchmarkString_UnicodeEscapes(b *testing.B) {
	json := []byte(`{"s":"Hello \u0041\u0042\u0043 World"}`)
	benchString(b, json)
}

func BenchmarkString_SurrogatePair(b *testing.B) {
	json := []byte(`{"s":"Emoji: \uD83D\uDE00 here"}`)
	benchString(b, json)
}

func BenchmarkString_EscapedInLargeDoc(b *testing.B) {
	// Short escaped string at the start of a large document
	largeValue := make([]byte, 1024*1024) // 1MB
	for i := range largeValue {
		largeValue[i] = 'A'
	}

	input := []byte(`{"s":"\n","big":"`)
	input = append(input, largeValue...)
	input = append(input, `"}`...)

	type T struct {
		S   string `json:"s"`
		Big string `json:"big"`
	}
	typ := reflect.TypeOf(T{})
	reg := BuildRegistry(typ)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result T
		ptr := unsafe.Pointer(&result)
		if err := ParseObject(input, reg, ptr); err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}
