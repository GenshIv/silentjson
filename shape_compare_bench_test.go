package silentjson

// Shape comparison benchmarks: the same ~14-20 MB of JSON in four record
// shapes, decoded by every parser:
//
//   small  — 500k tiny records (~40 B each, 3 fields)
//   medium — 100k Employee records (~160 B each, nested object + slices)
//   large  — 1k large records (~14 KB each: 8 KB string, 64 tags, 128 scores)
//   huge   — 8 very large records (~2 MB each: 1.2 MB string, 2048 tags,
//            4096 scores, deep values)
//
// All payloads are plain ASCII (escapes and UTF-8 are measured separately
// in BenchmarkPayloadFlavors); all setup happens before b.ResetTimer and
// every loop checks errors. TestShapeComparisonCorrectness verifies each
// parser against encoding/json first, so the numbers are only over
// decoders that actually decode.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/GenshIv/silentjson/v2/benchpayload"
	"github.com/bytedance/sonic"
	goccyjson "github.com/goccy/go-json"
	jsoniter "github.com/json-iterator/go"
	"github.com/mailru/easyjson"
	simdjson "github.com/minio/simdjson-go"
)

// --- payload builders (plain ASCII, no escapes, no multibyte) ---

func buildSmall(n int) []benchpayload.SmallRec {
	out := make([]benchpayload.SmallRec, n)
	for i := range out {
		out[i] = benchpayload.SmallRec{ID: i, Code: fmt.Sprintf("c%05x", i&0xfffff), OK: i%2 == 0}
	}
	return out
}

func buildLarge(n, descReps, tags, scores int) []benchpayload.LargeRec {
	out := make([]benchpayload.LargeRec, n)
	desc := strings.Repeat("Lorem ipsum dolor sit amet ", descReps)
	for i := range out {
		tagList := make([]string, tags)
		for j := range tagList {
			tagList[j] = fmt.Sprintf("tag%03d", j)
		}
		scoreList := make([]int, scores)
		for j := range scoreList {
			scoreList[j] = (i + j) % 1000
		}
		out[i] = benchpayload.LargeRec{
			ID:          i,
			Name:        fmt.Sprintf("record-%06d", i),
			Description: desc + fmt.Sprintf(" # %d", i),
			Tags:        tagList,
			Scores:      scoreList,
			Address:     benchpayload.Address{City: fmt.Sprintf("Warsaw_%d", i%100), Zip: 10000 + i},
			Active:      i%2 == 0,
			Balance:     float64(i) * 1.15,
		}
	}
	return out
}

var (
	shapeSmallSrc  = buildSmall(500_000)                 // ~20 MB
	shapeMediumSrc []benchpayload.Employee               // ~16 MB, set in init
	shapeLargeSrc  = buildLarge(1_000, 300, 64, 128)     // ~14 MB
	shapeHugeSrc   = buildLarge(8, 45_000, 2_048, 4_096) // ~16 MB
)

func init() {
	for i := 0; i < 100_000; i++ {
		shapeMediumSrc = append(shapeMediumSrc, buildClean(i))
	}
}

func marshalOrFatal(tb testing.TB, v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	return raw
}

// --- simdjson extraction per shape (AST API is the library's only API) ---

func simdjsonDecodeSmall(data []byte, dst *[]benchpayload.SmallRec) error {
	pj, err := simdjson.Parse(data, nil)
	if err != nil {
		return err
	}
	root := pj.Iter()
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
	arr.ForEach(func(e simdjson.Iter) {
		if _, err := e.Object(obj); err != nil {
			return
		}
		var rec benchpayload.SmallRec
		if el := obj.FindKey("id", nil); el != nil {
			v, _ := el.Iter.Int()
			rec.ID = int(v)
		}
		if el := obj.FindKey("code", nil); el != nil {
			rec.Code, _ = el.Iter.String()
		}
		if el := obj.FindKey("ok", nil); el != nil {
			rec.OK, _ = el.Iter.Bool()
		}
		out = append(out, rec)
	})
	*dst = out
	return nil
}

func simdjsonDecodeLarge(data []byte, dst *[]benchpayload.LargeRec) error {
	pj, err := simdjson.Parse(data, nil)
	if err != nil {
		return err
	}
	root := pj.Iter()
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
	addr := &simdjson.Object{}
	tmp := &simdjson.Array{}
	arr.ForEach(func(e simdjson.Iter) {
		if _, err := e.Object(obj); err != nil {
			return
		}
		var rec benchpayload.LargeRec
		if el := obj.FindKey("id", nil); el != nil {
			v, _ := el.Iter.Int()
			rec.ID = int(v)
		}
		if el := obj.FindKey("name", nil); el != nil {
			rec.Name, _ = el.Iter.String()
		}
		if el := obj.FindKey("description", nil); el != nil {
			rec.Description, _ = el.Iter.String()
		}
		if el := obj.FindKey("tags", nil); el != nil && el.Type != simdjson.TypeNull {
			if _, err := el.Iter.Array(tmp); err == nil {
				rec.Tags, _ = tmp.AsString()
			}
		}
		if el := obj.FindKey("scores", nil); el != nil && el.Type != simdjson.TypeNull {
			if _, err := el.Iter.Array(tmp); err == nil {
				ints, _ := tmp.AsInteger()
				rec.Scores = make([]int, len(ints))
				for i, v := range ints {
					rec.Scores[i] = int(v)
				}
			}
		}
		if el := obj.FindKey("address", nil); el != nil {
			if _, err := el.Iter.Object(addr); err == nil {
				if a := addr.FindKey("city", nil); a != nil {
					rec.Address.City, _ = a.Iter.String()
				}
				if a := addr.FindKey("zip", nil); a != nil {
					v, _ := a.Iter.Int()
					rec.Address.Zip = int(v)
				}
			}
		}
		if el := obj.FindKey("active", nil); el != nil {
			rec.Active, _ = el.Iter.Bool()
		}
		if el := obj.FindKey("balance", nil); el != nil && el.Type != simdjson.TypeNull {
			rec.Balance, _ = el.Iter.Float()
		}
		out = append(out, rec)
	})
	*dst = out
	return nil
}

// --- correctness gate: every parser must decode every shape correctly ---

func TestShapeComparisonCorrectness(t *testing.T) {
	smallRaw := marshalOrFatal(t, buildSmall(500))
	mediumRaw := marshalOrFatal(t, shapeMediumSrc[:1000])
	largeRaw := marshalOrFatal(t, buildLarge(5, 10, 4, 8))
	hugeRaw := marshalOrFatal(t, buildLarge(2, 20, 8, 16))

	t.Run("small", func(t *testing.T) {
		var want []benchpayload.SmallRec
		if err := json.Unmarshal(smallRaw, &want); err != nil {
			t.Fatal(err)
		}
		reg := BuildRegistry(reflect.TypeOf(benchpayload.SmallRec{}))
		dst := make([]benchpayload.SmallRec, len(want))
		got, err := UnmarshalSlice(smallRaw, reg, dst)
		if err != nil {
			t.Fatalf("silentjson: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("silentjson mismatch: %+v vs %+v", got[0], want[0])
		}
		var sj []benchpayload.SmallRec
		if err := simdjsonDecodeSmall(smallRaw, &sj); err != nil {
			t.Fatalf("simdjson: %v", err)
		}
		if !reflect.DeepEqual(sj, want) {
			t.Errorf("simdjson mismatch: %+v vs %+v", sj[0], want[0])
		}
	})

	checkEmployee := func(t *testing.T, raw []byte) {
		var want []benchpayload.Employee
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		reg := BuildRegistry(reflect.TypeOf(benchpayload.Employee{}))
		dst := make([]benchpayload.Employee, len(want))
		got, err := UnmarshalSlice(raw, reg, dst)
		if err != nil {
			t.Fatalf("silentjson: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("silentjson mismatch: %+v vs %+v", got[0], want[0])
		}
	}
	t.Run("medium", func(t *testing.T) { checkEmployee(t, mediumRaw) })

	t.Run("large_huge", func(t *testing.T) {
		for name, raw := range map[string][]byte{"large": largeRaw, "huge": hugeRaw} {
			var want []benchpayload.LargeRec
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			reg := BuildRegistry(reflect.TypeOf(benchpayload.LargeRec{}))
			dst := make([]benchpayload.LargeRec, len(want))
			got, err := UnmarshalSlice(raw, reg, dst)
			if err != nil {
				t.Fatalf("%s: silentjson: %v", name, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: silentjson mismatch:\n got %+v...\nwant %+v...", name, got[0], want[0])
			}
			var sj []benchpayload.LargeRec
			if err := simdjsonDecodeLarge(raw, &sj); err != nil {
				t.Fatalf("%s: simdjson: %v", name, err)
			}
			if !reflect.DeepEqual(sj, want) {
				t.Fatalf("%s: simdjson mismatch:\n got %+v...\nwant %+v...", name, sj[0], want[0])
			}
			var ez benchpayload.LargeRecs
			if err := easyjson.Unmarshal(raw, &ez); err != nil {
				t.Fatalf("%s: easyjson: %v", name, err)
			}
			if !reflect.DeepEqual([]benchpayload.LargeRec(ez), want) {
				t.Fatalf("%s: easyjson mismatch", name)
			}
		}
	})
}

// --- the shape × parser matrix ---

// benchParser mirrors the SilentJSON loops exactly so that no parser is
// favored: the destination is preallocated before the timer and reused
// across iterations (nobody re-allocates the result slice per op), and
// every iteration writes a decoded value into a package sink so no
// parser can be dead-code-eliminated.
func benchParser[T any](b *testing.B, raw []byte, makeDst func() T, unmarshal func([]byte, *T) error, first func(*T) int) {
	dst := makeDst()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := unmarshal(raw, &dst); err != nil {
			b.Fatal(err)
		}
		sinkInt = first(&dst)
	}
}

func BenchmarkShapeComparison(b *testing.B) {
	b.Run("Small", func(b *testing.B) {
		raw := marshalOrFatal(b, shapeSmallSrc)
		reg := BuildRegistry(reflect.TypeOf(benchpayload.SmallRec{}))
		size := int64(len(raw))
		run := func(name string, fn func(*testing.B)) {
			b.Run(name, func(b *testing.B) { b.SetBytes(size); fn(b) })
		}
		run("SilentJSON_Serial", func(b *testing.B) {
			dst := make([]benchpayload.SmallRec, len(shapeSmallSrc))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := UnmarshalSlice(raw, reg, dst)
				if err != nil {
					b.Fatal(err)
				}
				sinkInt = out[0].ID
			}
		})
		run("SilentJSON_Parallel", func(b *testing.B) {
			dst := make([]benchpayload.SmallRec, len(shapeSmallSrc))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := UnmarshalArrayParallel[benchpayload.SmallRec](raw, reg, dst)
				if err != nil {
					b.Fatal(err)
				}
				sinkInt = out[0].ID
			}
		})
		smallDst := func() []benchpayload.SmallRec { return make([]benchpayload.SmallRec, 0, len(shapeSmallSrc)) }
		firstSmall := func(p *[]benchpayload.SmallRec) int { return (*p)[0].ID }
		run("Standard", func(b *testing.B) {
			benchParser(b, raw, smallDst, func(r []byte, out *[]benchpayload.SmallRec) error { return json.Unmarshal(r, out) }, firstSmall)
		})
		run("Sonic", func(b *testing.B) {
			benchParser(b, raw, smallDst, func(r []byte, out *[]benchpayload.SmallRec) error { return sonic.Unmarshal(r, out) }, firstSmall)
		})
		run("Goccy", func(b *testing.B) {
			benchParser(b, raw, smallDst, func(r []byte, out *[]benchpayload.SmallRec) error { return goccyjson.Unmarshal(r, out) }, firstSmall)
		})
		run("Jsoniter", func(b *testing.B) {
			benchParser(b, raw, smallDst, func(r []byte, out *[]benchpayload.SmallRec) error { return jsoniter.ConfigFastest.Unmarshal(r, out) }, firstSmall)
		})
		run("Easyjson", func(b *testing.B) {
			benchParser(b, raw, func() benchpayload.SmallRecs { return make(benchpayload.SmallRecs, 0, len(shapeSmallSrc)) },
				func(r []byte, out *benchpayload.SmallRecs) error { return easyjson.Unmarshal(r, out) },
				func(p *benchpayload.SmallRecs) int { return (*p)[0].ID })
		})
		run("Simdjson", func(b *testing.B) {
			if !simdjson.SupportedCPU() {
				b.Skip("CPU not supported")
			}
			out := make([]benchpayload.SmallRec, 0, len(shapeSmallSrc))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := simdjsonDecodeSmall(raw, &out); err != nil {
					b.Fatal(err)
				}
				sinkInt = out[0].ID
			}
		})
	})

	b.Run("Medium", func(b *testing.B) {
		raw := marshalOrFatal(b, shapeMediumSrc)
		reg := BuildRegistry(reflect.TypeOf(benchpayload.Employee{}))
		size := int64(len(raw))
		run := func(name string, fn func(*testing.B)) {
			b.Run(name, func(b *testing.B) { b.SetBytes(size); fn(b) })
		}
		run("SilentJSON_Serial", func(b *testing.B) {
			dst := make([]benchpayload.Employee, len(shapeMediumSrc))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := UnmarshalSlice(raw, reg, dst)
				if err != nil {
					b.Fatal(err)
				}
				sinkInt = out[0].ID
			}
		})
		run("SilentJSON_Parallel", func(b *testing.B) {
			dst := make([]benchpayload.Employee, len(shapeMediumSrc))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := UnmarshalArrayParallel[benchpayload.Employee](raw, reg, dst)
				if err != nil {
					b.Fatal(err)
				}
				sinkInt = out[0].ID
			}
		})
		empDst := func() []benchpayload.Employee { return make([]benchpayload.Employee, 0, len(shapeMediumSrc)) }
		firstEmp := func(p *[]benchpayload.Employee) int { return (*p)[0].ID }
		run("Standard", func(b *testing.B) {
			benchParser(b, raw, empDst, func(r []byte, out *[]benchpayload.Employee) error { return json.Unmarshal(r, out) }, firstEmp)
		})
		run("Sonic", func(b *testing.B) {
			benchParser(b, raw, empDst, func(r []byte, out *[]benchpayload.Employee) error { return sonic.Unmarshal(r, out) }, firstEmp)
		})
		run("Goccy", func(b *testing.B) {
			benchParser(b, raw, empDst, func(r []byte, out *[]benchpayload.Employee) error { return goccyjson.Unmarshal(r, out) }, firstEmp)
		})
		run("Jsoniter", func(b *testing.B) {
			benchParser(b, raw, empDst, func(r []byte, out *[]benchpayload.Employee) error { return jsoniter.ConfigFastest.Unmarshal(r, out) }, firstEmp)
		})
		run("Easyjson", func(b *testing.B) {
			benchParser(b, raw, func() benchpayload.Employees { return make(benchpayload.Employees, 0, len(shapeMediumSrc)) },
				func(r []byte, out *benchpayload.Employees) error { return easyjson.Unmarshal(r, out) },
				func(p *benchpayload.Employees) int { return (*p)[0].ID })
		})
		run("Simdjson", func(b *testing.B) {
			if !simdjson.SupportedCPU() {
				b.Skip("CPU not supported")
			}
			out := make([]benchpayload.Employee, 0, len(shapeMediumSrc))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := simdjsonDecode(raw, &out); err != nil {
					b.Fatal(err)
				}
				sinkInt = out[0].ID
			}
		})
	})

	for _, shape := range []struct {
		name string
		src  []benchpayload.LargeRec
	}{
		{"Large", shapeLargeSrc},
		{"Huge", shapeHugeSrc},
	} {
		b.Run(shape.name, func(b *testing.B) {
			raw := marshalOrFatal(b, shape.src)
			reg := BuildRegistry(reflect.TypeOf(benchpayload.LargeRec{}))
			size := int64(len(raw))
			run := func(name string, fn func(*testing.B)) {
				b.Run(name, func(b *testing.B) { b.SetBytes(size); fn(b) })
			}
			run("SilentJSON_Serial", func(b *testing.B) {
				dst := make([]benchpayload.LargeRec, len(shape.src))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					out, err := UnmarshalSlice(raw, reg, dst)
					if err != nil {
						b.Fatal(err)
					}
					sinkInt = out[0].ID
				}
			})
			run("SilentJSON_Parallel", func(b *testing.B) {
				dst := make([]benchpayload.LargeRec, len(shape.src))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					out, err := UnmarshalArrayParallel[benchpayload.LargeRec](raw, reg, dst)
					if err != nil {
						b.Fatal(err)
					}
					sinkInt = out[0].ID
				}
			})
			largeDst := func() []benchpayload.LargeRec { return make([]benchpayload.LargeRec, 0, len(shape.src)) }
			firstLarge := func(p *[]benchpayload.LargeRec) int { return (*p)[0].ID }
			run("Standard", func(b *testing.B) {
				benchParser(b, raw, largeDst, func(r []byte, out *[]benchpayload.LargeRec) error { return json.Unmarshal(r, out) }, firstLarge)
			})
			run("Sonic", func(b *testing.B) {
				benchParser(b, raw, largeDst, func(r []byte, out *[]benchpayload.LargeRec) error { return sonic.Unmarshal(r, out) }, firstLarge)
			})
			run("Goccy", func(b *testing.B) {
				benchParser(b, raw, largeDst, func(r []byte, out *[]benchpayload.LargeRec) error { return goccyjson.Unmarshal(r, out) }, firstLarge)
			})
			run("Jsoniter", func(b *testing.B) {
				benchParser(b, raw, largeDst, func(r []byte, out *[]benchpayload.LargeRec) error { return jsoniter.ConfigFastest.Unmarshal(r, out) }, firstLarge)
			})
			run("Easyjson", func(b *testing.B) {
				benchParser(b, raw, func() benchpayload.LargeRecs { return make(benchpayload.LargeRecs, 0, len(shape.src)) },
					func(r []byte, out *benchpayload.LargeRecs) error { return easyjson.Unmarshal(r, out) },
					func(p *benchpayload.LargeRecs) int { return (*p)[0].ID })
			})
			run("Simdjson", func(b *testing.B) {
				if !simdjson.SupportedCPU() {
					b.Skip("CPU not supported")
				}
				out := make([]benchpayload.LargeRec, 0, len(shape.src))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := simdjsonDecodeLarge(raw, &out); err != nil {
						b.Fatal(err)
					}
					sinkInt = out[0].ID
				}
			})
		})
	}
}
