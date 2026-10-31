package silentjson

// Payload-flavor benchmarks: the same 100k-record array rendered three
// ways, to separate the cost of individual payload properties:
//
//   - clean:   plain ASCII strings, no escapes, no multibyte runes
//   - escaped: same records, ~10% of strings contain \" and \\ sequences
//   - utf8:    same records, strings contain multibyte (2-4 byte) runes
//
// The parser performs UTF-16/UTF-8 work only on the \uXXXX branch of an
// escaped string; plain and multibyte-but-unescaped strings take the
// zero-copy path with no unicode validation at all. These benchmarks pin
// down what each flavor actually costs.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/GenshIv/silentjson/v2/benchpayload"
)

type payloadFlavor struct {
	name  string
	build func(i int) benchpayload.Employee
}

func buildClean(i int) benchpayload.Employee {
	return benchpayload.Employee{
		ID:       i,
		IsActive: i%2 == 0,
		Balance:  float64(i) * 1.15,
		Address:  benchpayload.Address{City: fmt.Sprintf("Warsaw_%d", i%100), Zip: 10000 + i},
		Tags:     []string{"highload", "go", "unsafe", fmt.Sprintf("%d", i)},
		Scores:   []int{i % 10, i % 20, i % 30},
	}
}

var payloadFlavors = []payloadFlavor{
	{"clean", buildClean},
	{"escaped", func(i int) benchpayload.Employee {
		e := buildClean(i)
		if i%10 == 0 {
			// json.Marshal renders these as \" and \\ escape sequences.
			e.Address.City = fmt.Sprintf(`Warsaw \"Central\" %d`, i)
		}
		return e
	}},
	{"utf8", func(i int) benchpayload.Employee {
		e := buildClean(i)
		if i%10 == 0 {
			// Multibyte but UNESCAPED runes: exercise UTF-8 pass-through,
			// not the escape decoder.
			e.Address.City = fmt.Sprintf("Варшава_中央_%d", i)
			e.Tags = []string{"highload", "go", "łąka", fmt.Sprintf("%d", i)}
		}
		return e
	}},
}

// TestPayloadFlavorRoundTrip verifies every flavor decodes correctly —
// a benchmark over a payload the parser cannot round-trip is worthless.
func TestPayloadFlavorRoundTrip(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(benchpayload.Employee{}))
	for _, f := range payloadFlavors {
		src := make([]benchpayload.Employee, 1000)
		for i := range src {
			src[i] = f.build(i)
		}
		raw, err := json.Marshal(src)
		if err != nil {
			t.Fatalf("%s: marshal: %v", f.name, err)
		}
		// json.Marshal itself escapes nothing for these flavors (no HTML
		// chars, no control chars) except the escaped flavor's \" and \\.
		dst := make([]benchpayload.Employee, len(src))
		out, err := UnmarshalSlice(raw, reg, dst)
		if err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		for i := range out {
			if out[i].ID != src[i].ID || out[i].Address.City != src[i].Address.City ||
				len(out[i].Tags) != len(src[i].Tags) || out[i].Tags[0] != src[i].Tags[0] {
				t.Fatalf("%s: record %d mismatch: %+v vs %+v", f.name, i, out[i], src[i])
			}
		}
	}
}

// BenchmarkPayloadFlavors decodes each flavor with SilentJSON (serial and
// parallel) and encoding/json for reference. All setup happens before
// b.ResetTimer; errors are checked so only real work is measured.
func BenchmarkPayloadFlavors(b *testing.B) {
	reg := BuildRegistry(reflect.TypeOf(benchpayload.Employee{}))
	const n = 100_000

	for _, f := range payloadFlavors {
		src := make([]benchpayload.Employee, n)
		for i := range src {
			src[i] = f.build(i)
		}
		raw, err := json.Marshal(src)
		if err != nil {
			b.Fatalf("%s: marshal: %v", f.name, err)
		}
		size := int64(len(raw))

		b.Run("SilentJSON_Serial/"+f.name, func(b *testing.B) {
			dst := make([]benchpayload.Employee, n)
			b.SetBytes(size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := UnmarshalSlice(raw, reg, dst); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run("SilentJSON_Parallel/"+f.name, func(b *testing.B) {
			dst := make([]benchpayload.Employee, n)
			b.SetBytes(size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := UnmarshalArrayParallel[benchpayload.Employee](raw, reg, dst); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run("Standard/"+f.name, func(b *testing.B) {
			dst := make([]benchpayload.Employee, n)
			b.SetBytes(size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := json.Unmarshal(raw, &dst); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
