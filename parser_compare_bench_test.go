package silentjson

// Cross-parser comparison benchmarks: silentjson vs simdjson-go, easyjson,
// sonic, goccy, jsoniter and encoding/json on the identical payload.
//
// Benchmark hygiene rules applied to every sub-benchmark below:
//   - payload generation, type registries, destination buffers and reusable
//     parser objects are created BEFORE b.ResetTimer, never inside the
//     b.N loop;
//   - the payload is generated once (init) and shared; silentjson treats its
//     input as read-only, so no defensive copies are made;
//   - results are stored into package-level sinks so the compiler cannot
//     eliminate the parsing work.
//
// simdjson-go has no reflection binding, so its benchmark extracts fields
// through the element API — the idiomatic usage of that library. The
// extraction is verified against encoding/json in
// TestParserComparisonCorrectness, so the benchmark measures real decoding,
// not a broken loop.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/GenshIv/silentjson/v2/benchpayload"
	"github.com/bytedance/sonic"
	goccyjson "github.com/goccy/go-json"
	jsoniter "github.com/json-iterator/go"
	"github.com/mailru/easyjson"
	simdjson "github.com/minio/simdjson-go"
)

// --- payload, built once at process start (never inside a benchmark) ---

var (
	parserCmpSource []benchpayload.Employee // objects used to build the payload
	parserCmpJSON   []byte                  // the shared serialized payload
)

func init() {
	const n = benchSliceSize
	parserCmpSource = make([]benchpayload.Employee, n)
	var buf bytes.Buffer
	buf.WriteString("[")
	for i := 0; i < n; i++ {
		// Payload mirrors the "chaos" shape of the other benches: rotating
		// key order, unknown fields, escaped strings, empty arrays and nulls.
		city := fmt.Sprintf("Warsaw_%d", i%100)
		if i%97 == 0 {
			city = fmt.Sprintf("Warsaw \\\"Central\\\" %d", i)
		}
		tagsStr := `"backend","go","fast"`
		scoresStr := "10,20,30"
		if i%7 == 0 {
			tagsStr = ""
		}
		if i%11 == 0 {
			scoresStr = ""
		}
		balanceStr := fmt.Sprintf("%.2f", float64(i)*1.15)
		if i%13 == 0 {
			balanceStr = "null"
		}

		var line string
		switch i % 3 {
		case 0:
			line = fmt.Sprintf(`{"id":%d,"unknown_arr":[1,2,{"nest":true}],"is_active":%t,"balance":%s,"address":{"city":"%s","zip":%d},"tags":[%s],"scores":[%s]}`,
				i, i%2 == 0, balanceStr, city, 10000+i, tagsStr, scoresStr)
		case 1:
			line = fmt.Sprintf(`{"scores":[%s],"address":{"zip":%d,"city":"%s"},"junk":{"foo":"bar"},"balance":%s,"tags":[%s],"is_active":%t,"id":%d}`,
				scoresStr, 10000+i, city, balanceStr, tagsStr, i%2 == 0, i)
		case 2:
			line = fmt.Sprintf(`{"is_active":%t,"tags":[%s],"id":%d,"ignore_me":null,"scores":[%s],"balance":%s,"address":{"zip":%d,"city":"%s"}}`,
				i%2 == 0, tagsStr, i, scoresStr, balanceStr, 10000+i, city)
		}
		buf.WriteString(line)
		if i < n-1 {
			buf.WriteString(",")
		}

		// Source objects reference the same values for the correctness test.
		parserCmpSource[i] = benchpayload.Employee{
			ID:       i,
			IsActive: i%2 == 0,
			Balance:  float64(i) * 1.15,
			Address:  benchpayload.Address{City: city, Zip: 10000 + i},
			Tags:     []string{"backend", "go", "fast"},
			Scores:   []int{10, 20, 30},
		}
	}
	buf.WriteString("]")
	parserCmpJSON = buf.Bytes()
}

// sinks keep the decode loops alive across compiler optimizations
var (
	sinkEmployees []benchpayload.Employee
	sinkInt       int
)

// TestParserComparisonCorrectness verifies that every parser benchmarked
// below decodes the shared payload into the same values as encoding/json.
// In particular it validates the hand-written simdjson extraction loop —
// a benchmark measuring a broken extractor would be worse than none.
func TestParserComparisonCorrectness(t *testing.T) {
	// All records carry identical Tags/Scores in the payload; balances are
	// either %.2f text or null. Compare a deterministic sample of records.
	want := make([]benchpayload.Employee, len(parserCmpSource))
	if err := json.Unmarshal(parserCmpJSON, &want); err != nil {
		t.Fatalf("encoding/json reference decode: %v", err)
	}

	check := func(name string, got []benchpayload.Employee) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: decoded %d records, want %d", name, len(got), len(want))
		}
		for i := range got {
			g, w := got[i], want[i]
			if g.ID != w.ID || g.IsActive != w.IsActive || g.Balance != w.Balance ||
				g.Address.City != w.Address.City || g.Address.Zip != w.Address.Zip {
				t.Fatalf("%s: record %d mismatch:\n got %+v\nwant %+v", name, i, g, w)
			}
			if len(g.Tags) != len(w.Tags) || len(g.Scores) != len(w.Scores) {
				t.Fatalf("%s: record %d slice len mismatch: %+v vs %+v", name, i, g, w)
			}
			for j := range g.Tags {
				if g.Tags[j] != w.Tags[j] {
					t.Fatalf("%s: record %d tags mismatch: %q vs %q", name, i, g.Tags[j], w.Tags[j])
				}
			}
			for j := range g.Scores {
				if g.Scores[j] != w.Scores[j] {
					t.Fatalf("%s: record %d scores mismatch", name, i)
				}
			}
		}
	}

	// silentjson: UnmarshalSlice fills dst[:count] and requires len(dst) >= count.
	reg := BuildRegistry(reflect.TypeOf(benchpayload.Employee{}))
	got := make([]benchpayload.Employee, len(parserCmpSource))
	out, err := UnmarshalSlice(parserCmpJSON, reg, got)
	if err != nil {
		t.Fatalf("silentjson decode: %v", err)
	}
	check("silentjson", out)

	// easyjson
	var ez benchpayload.Employees
	if err := easyjson.Unmarshal(parserCmpJSON, &ez); err != nil {
		t.Fatalf("easyjson decode: %v", err)
	}
	check("easyjson", ez)

	// sonic
	var so []benchpayload.Employee
	if err := sonic.Unmarshal(parserCmpJSON, &so); err != nil {
		t.Fatalf("sonic decode: %v", err)
	}
	check("sonic", so)

	// simdjson-go (extraction used by the benchmark)
	if !simdjson.SupportedCPU() {
		t.Skip("simdjson-go: CPU not supported")
	}
	var sj []benchpayload.Employee
	if err := simdjsonDecode(parserCmpJSON, &sj); err != nil {
		t.Fatalf("simdjson decode: %v", err)
	}
	check("simdjson", sj)
}

// simdjsonDecode extracts benchpayload.Employee records from a root JSON
// array using the simdjson-go element API (the library has no reflection
// binding; this is its idiomatic usage).
func simdjsonDecode(data []byte, dst *[]benchpayload.Employee) error {
	pj, err := simdjson.Parse(data, nil)
	if err != nil {
		return err
	}
	root := pj.Iter()
	// Library pattern: AdvanceInto() loads the synthetic root token,
	// Root(nil) descends to the actual top-level value.
	root.AdvanceInto()
	t, top, err := root.Root(nil)
	if err != nil {
		return err
	}
	if t != simdjson.TypeArray {
		return fmt.Errorf("root is %v, want array", t)
	}
	arr, err := top.Array(nil)
	if err != nil {
		return err
	}
	out := (*dst)[:0]
	obj := &simdjson.Object{}
	addrObj := &simdjson.Object{}
	tmpArr := &simdjson.Array{}
	var extractErr error
	arr.ForEach(func(e simdjson.Iter) {
		if extractErr != nil {
			return
		}
		if _, err := e.Object(obj); err != nil {
			extractErr = err
			return
		}
		var emp benchpayload.Employee
		if el := obj.FindKey("id", nil); el != nil {
			v, err := el.Iter.Int()
			if err != nil {
				extractErr = err
				return
			}
			emp.ID = int(v)
		}
		if el := obj.FindKey("is_active", nil); el != nil {
			v, err := el.Iter.Bool()
			if err != nil {
				extractErr = err
				return
			}
			emp.IsActive = v
		}
		if el := obj.FindKey("balance", nil); el != nil && el.Type != simdjson.TypeNull {
			v, err := el.Iter.Float()
			if err != nil {
				extractErr = err
				return
			}
			emp.Balance = v
		}
		if el := obj.FindKey("address", nil); el != nil {
			if _, err := el.Iter.Object(addrObj); err != nil {
				extractErr = err
				return
			}
			if a := addrObj.FindKey("city", nil); a != nil {
				if v, err := a.Iter.String(); err == nil {
					emp.Address.City = v
				} else {
					extractErr = err
					return
				}
			}
			if a := addrObj.FindKey("zip", nil); a != nil {
				v, err := a.Iter.Int()
				if err != nil {
					extractErr = err
					return
				}
				emp.Address.Zip = int(v)
			}
		}
		if el := obj.FindKey("tags", nil); el != nil && el.Type != simdjson.TypeNull {
			if _, err := el.Iter.Array(tmpArr); err != nil {
				extractErr = err
				return
			}
			if emp.Tags, err = tmpArr.AsString(); err != nil {
				extractErr = err
				return
			}
		}
		if el := obj.FindKey("scores", nil); el != nil && el.Type != simdjson.TypeNull {
			if _, err := el.Iter.Array(tmpArr); err != nil {
				extractErr = err
				return
			}
			ints, err := tmpArr.AsInteger()
			if err != nil {
				extractErr = err
				return
			}
			emp.Scores = make([]int, len(ints))
			for i, v := range ints {
				emp.Scores[i] = int(v)
			}
		}
		out = append(out, emp)
	})
	if extractErr != nil {
		return extractErr
	}
	*dst = out
	return nil
}

// BenchmarkParserComparison deserializes the same ~16 MB payload
// (100k Employee records, chaos mode) with every parser.
// All setup (payload, registries, destinations, reusable parser state)
// happens before b.ResetTimer.
func BenchmarkParserComparison(b *testing.B) {
	// shared setup, outside every timer
	reg := BuildRegistry(reflect.TypeOf(benchpayload.Employee{}))
	dst := make([]benchpayload.Employee, len(parserCmpSource))
	size := int64(len(parserCmpJSON))

	b.Run("SilentJSON_Serial", func(b *testing.B) {
		b.SetBytes(size)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			out, err := UnmarshalSlice(parserCmpJSON, reg, dst)
			if err != nil {
				b.Fatal(err)
			}
			sinkInt = out[0].ID
		}
	})

	b.Run("SilentJSON_Parallel", func(b *testing.B) {
		b.SetBytes(size)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			out, err := UnmarshalArrayParallel[benchpayload.Employee](parserCmpJSON, reg, dst)
			if err != nil {
				b.Fatal(err)
			}
			sinkInt = out[0].ID
		}
	})

	b.Run("Easyjson", func(b *testing.B) {
		b.SetBytes(size)
		b.ReportAllocs()
		b.ResetTimer()
		var out benchpayload.Employees
		for i := 0; i < b.N; i++ {
			out = out[:0]
			if err := easyjson.Unmarshal(parserCmpJSON, &out); err != nil {
				b.Fatal(err)
			}
			sinkInt = out[0].ID
		}
	})

	b.Run("Simdjson", func(b *testing.B) {
		if !simdjson.SupportedCPU() {
			b.Skip("CPU not supported by simdjson-go")
		}
		b.SetBytes(size)
		b.ReportAllocs()
		b.ResetTimer()
		var out []benchpayload.Employee
		for i := 0; i < b.N; i++ {
			if err := simdjsonDecode(parserCmpJSON, &out); err != nil {
				b.Fatal(err)
			}
			sinkInt = out[0].ID
		}
	})

	b.Run("Sonic", func(b *testing.B) {
		b.SetBytes(size)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := sonic.Unmarshal(parserCmpJSON, &dst); err != nil {
				b.Fatal(err)
			}
			sinkInt = dst[0].ID
		}
	})

	b.Run("Goccy", func(b *testing.B) {
		b.SetBytes(size)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := goccyjson.Unmarshal(parserCmpJSON, &dst); err != nil {
				b.Fatal(err)
			}
			sinkInt = dst[0].ID
		}
	})

	b.Run("Jsoniter", func(b *testing.B) {
		b.SetBytes(size)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var out []benchpayload.Employee
			if err := jsoniter.ConfigFastest.Unmarshal(parserCmpJSON, &out); err != nil {
				b.Fatal(err)
			}
			sinkInt = out[0].ID
		}
	})

	b.Run("Standard", func(b *testing.B) {
		b.SetBytes(size)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := json.Unmarshal(parserCmpJSON, &dst); err != nil {
				b.Fatal(err)
			}
			sinkInt = dst[0].ID
		}
	})
}
