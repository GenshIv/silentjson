package silentjson

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

type benchString struct {
	S string `json:"s"`
}

func BenchmarkReadJSONString_NoEscape(b *testing.B) {
	src := `{"s":"` + strings.Repeat("a", 10000) + `"}`
	reg := BuildRegistry(reflect.TypeOf(benchString{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v benchString
		if err := ParseObject([]byte(src), reg, unsafe.Pointer(&v)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadJSONString_EscapeStart(b *testing.B) {
	src := `{"s":"\n` + strings.Repeat("a", 9990) + `"}`
	reg := BuildRegistry(reflect.TypeOf(benchString{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v benchString
		if err := ParseObject([]byte(src), reg, unsafe.Pointer(&v)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadJSONString_EscapeMiddle(b *testing.B) {
	src := `{"s":"` + strings.Repeat("a", 5000) + `\n` + strings.Repeat("b", 4990) + `"}`
	reg := BuildRegistry(reflect.TypeOf(benchString{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v benchString
		if err := ParseObject([]byte(src), reg, unsafe.Pointer(&v)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadJSONString_EscapeEnd(b *testing.B) {
	src := `{"s":"` + strings.Repeat("a", 9990) + `\n"}`
	reg := BuildRegistry(reflect.TypeOf(benchString{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v benchString
		if err := ParseObject([]byte(src), reg, unsafe.Pointer(&v)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadJSONString_ManyEscapes(b *testing.B) {
	src := `{"s":"` + strings.Repeat(`a\n`, 5000) + `"}`
	reg := BuildRegistry(reflect.TypeOf(benchString{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v benchString
		if err := ParseObject([]byte(src), reg, unsafe.Pointer(&v)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadJSONString_UnicodeEscapes(b *testing.B) {
	src := `{"s":"` + strings.Repeat(`\u0041`, 5000) + `"}`
	reg := BuildRegistry(reflect.TypeOf(benchString{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v benchString
		if err := ParseObject([]byte(src), reg, unsafe.Pointer(&v)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadJSONString_SurrogatePairs(b *testing.B) {
	src := `{"s":"` + strings.Repeat(`\ud83d\ude00`, 1000) + `"}`
	reg := BuildRegistry(reflect.TypeOf(benchString{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v benchString
		if err := ParseObject([]byte(src), reg, unsafe.Pointer(&v)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadJSONString_Mixed(b *testing.B) {
	src := `{"s":"` + strings.Repeat(`a\nb\tc\rd`, 1000) + `"}`
	reg := BuildRegistry(reflect.TypeOf(benchString{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v benchString
		if err := ParseObject([]byte(src), reg, unsafe.Pointer(&v)); err != nil {
			b.Fatal(err)
		}
	}
}
