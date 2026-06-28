package silentjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/GenshIv/silentjson/pb"
	"github.com/bytedance/sonic"
	simdjson "github.com/minio/simdjson-go"
	"google.golang.org/protobuf/proto"
)

// --- STRUCTURES AND GENERATOR ---

type Address struct {
	City string `json:"city"`
	Zip  int    `json:"zip"`
}

type Employee struct {
	ID       int      `json:"id"`
	IsActive bool     `json:"is_active"`
	Balance  float64  `json:"balance"`
	Address  Address  `json:"address"`
	Tags     []string `json:"tags"`
	Scores   []int    `json:"scores"`
}

const (
	recordCount = 3_000_000

	benchSliceSize = 100_000
)

var testFileName = fmt.Sprintf("nested_huge_data_%d.json", recordCount)

type EmpSlice []Employee

var (
	hugeJSONData  []byte
	benchEmpSlice []Employee
)

func init() {
	if _, err := os.Stat(testFileName); os.IsNotExist(err) {
		fmt.Printf("Generating file %s (Chaos-mode)...\n", testFileName)
		file, _ := os.Create(testFileName)
		file.WriteString("[\n")

		escapedCount := 0

		for i := 0; i < recordCount; i++ {
			city := fmt.Sprintf("Warsaw_%d", i%100)
			if escapedCount < 100 {
				city = fmt.Sprintf("Warsaw \\\"Central\\\" %d", i)
				escapedCount++
			}
			if i%17 == 0 {
				city = ""
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

			file.WriteString(line)
			if i < recordCount-1 {
				file.WriteString(",\n")
			} else {
				file.WriteString("\n")
			}
		}
		file.WriteString("]\n")
		file.Close()
	}

	hugeJSONData, _ = os.ReadFile(testFileName)
	printMemoryAnalytics()
	initMarshalData()
}

func printMemoryAnalytics() {
	baseStructSize := unsafe.Sizeof(Employee{})
	sliceBackingArraysSize := uintptr(4*16 + 3*8)
	totalPerRecord := baseStructSize + sliceBackingArraysSize
	totalMB := float64(totalPerRecord*uintptr(recordCount)) / 1024 / 1024
	bufferMB := float64(len(hugeJSONData)) / 1024 / 1024

	fmt.Printf("\n=== MEMORY ANALYTICS ===\n")
	fmt.Printf("1. Source JSON buffer (Zero-Copy Source): %.2f MB\n", bufferMB)
	fmt.Printf("2. Average size of Employee + arrays: %d bytes\n", totalPerRecord)
	fmt.Printf("3. Total RAM for %d records: %.2f MB\n", recordCount, totalMB)
	fmt.Printf("================================\n\n")
}

func initMarshalData() {
	fmt.Printf("Generating slice of %d structures for Marshal...\n", benchSliceSize)
	benchEmpSlice = make([]Employee, benchSliceSize)
	for i := 0; i < benchSliceSize; i++ {
		city := fmt.Sprintf("Warsaw_%d", i%100)
		if i%10 == 0 {
			city = fmt.Sprintf("Warsaw \\\"Central\\\" %d", i)
		}

		benchEmpSlice[i] = Employee{
			ID:       i,
			IsActive: i%2 == 0,
			Balance:  float64(i) * 1.15,
			Address: Address{
				City: city,
				Zip:  10000 + i,
			},
			Tags:   []string{"highload", "go", "unsafe", fmt.Sprintf("%d", i)},
			Scores: []int{i % 10, i % 20, i % 30},
		}
	}
	fmt.Println("Marshal slice is ready!")
}

// --- BENCHMARKS ---

// BenchmarkNestedComparison tests sequential deserialization (unmarshal)
// of a huge JSON file (3,000,000 records, Chaos-mode) with deep nesting,
// escaped strings, null values, and unknown fields.
// Compares: SilentJSON (sequential), Sonic, encoding/json (Standard), and simdjson-go.
func BenchmarkNestedComparison(b *testing.B) {
	b.Run("SilentJSON", func(b *testing.B) {
		reg := BuildRegistry(reflect.TypeOf(Employee{}))
		dst := make([]Employee, recordCount)
		for i := range dst {
			dst[i].Tags = make([]string, 0, 4)
			dst[i].Scores = make([]int, 0, 4)
		}

		buf := make([]byte, len(hugeJSONData))

		b.SetBytes(int64(len(hugeJSONData)))
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			copy(buf, hugeJSONData)
			_, err := UnmarshalSlice(buf, reg, dst)
			if err != nil {
				b.Error(err)
			}
		}
	})

	b.Run("Sonic", func(b *testing.B) {
		dst := make([]Employee, recordCount)
		b.SetBytes(int64(len(hugeJSONData)))
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			err := sonic.Unmarshal(hugeJSONData, &dst)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Standard", func(b *testing.B) {
		b.SetBytes(int64(len(hugeJSONData)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reader := bytes.NewReader(hugeJSONData)
			dec := json.NewDecoder(reader)
			_, _ = dec.Token()
			var emp Employee
			for dec.More() {
				_ = dec.Decode(&emp)
			}
		}
	})

	b.Run("Simdjson", func(b *testing.B) {
		b.SetBytes(int64(len(hugeJSONData)))
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			parsed, err := simdjson.Parse(hugeJSONData, nil)
			if err != nil {
				b.Fatal(err)
			}
			iter := parsed.Iter()
			if iter.Type() == simdjson.TypeArray {
				array, _ := iter.Array(nil)
				array.ForEach(func(i simdjson.Iter) {
					if i.Type() == simdjson.TypeObject {
						obj, _ := i.Object(nil)
						var emp Employee

						var elem simdjson.Element

						if obj.FindKey("id", &elem) != nil {
							id, _ := elem.Iter.Int()
							emp.ID = int(id)
						}
						if obj.FindKey("is_active", &elem) != nil {
							emp.IsActive, _ = elem.Iter.Bool()
						}
						if obj.FindKey("balance", &elem) != nil {
							emp.Balance, _ = elem.Iter.Float()
						}
						if obj.FindKey("address", &elem) != nil {
							if elem.Type == simdjson.TypeObject {
								addrObj, _ := elem.Iter.Object(nil)
								var subElem simdjson.Element
								if addrObj.FindKey("city", &subElem) != nil {
									emp.Address.City, _ = subElem.Iter.String()
								}
								if addrObj.FindKey("zip", &subElem) != nil {
									zip, _ := subElem.Iter.Int()
									emp.Address.Zip = int(zip)
								}
							}
						}
						if obj.FindKey("tags", &elem) != nil {
							if elem.Type == simdjson.TypeArray {
								tagsArr, _ := elem.Iter.Array(nil)
								tagsArr.ForEach(func(t simdjson.Iter) {
									tagStr, _ := t.String()
									emp.Tags = append(emp.Tags, tagStr)
								})
							}
						}
						if obj.FindKey("scores", &elem) != nil {
							if elem.Type == simdjson.TypeArray {
								scoresArr, _ := elem.Iter.Array(nil)
								scoresArr.ForEach(func(s simdjson.Iter) {
									scoreInt, _ := s.Int()
									emp.Scores = append(emp.Scores, int(scoreInt))
								})
							}
						}
						_ = emp
					}
				})
			}
		}
	})
}

// BenchmarkLargeScaleGeneration tests serialization (marshal/generation)
// of a large struct array (100,000 Employee objects) to JSON and Protobuf formats.
// Compares: SilentJSON (MarshalSlice with buffer reuse), Sonic,
// encoding/json (Standard), and Protobuf (proto.Marshal).
func BenchmarkLargeScaleGeneration(b *testing.B) {
	// Prepare Protobuf structure
	pbEmployees := &pb.Employees{
		List: make([]*pb.Employee, len(benchEmpSlice)),
	}
	for i, emp := range benchEmpSlice {
		pbEmployees.List[i] = &pb.Employee{
			Id:       int32(emp.ID),
			IsActive: emp.IsActive,
			Balance:  emp.Balance,
			Address: &pb.Address{
				City: emp.Address.City,
				Zip:  int32(emp.Address.Zip),
			},
			Tags:   emp.Tags,
			Scores: sliceIntToInt64(emp.Scores),
		}
	}

	b.Run("SilentJSON", func(b *testing.B) {
		const InitialCap = 25 * 1024 * 1024 // 25 MB
		const MaxCap = 100 * 1024 * 1024    // 100 MB - limit for reset

		reg := BuildRegistry(reflect.TypeOf(Employee{}))
		buf := make([]byte, 0, InitialCap)

		buf = MarshalSlice(benchEmpSlice, reg, buf)
		b.SetBytes(int64(len(buf)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if cap(buf) > MaxCap {
				buf = make([]byte, 0, InitialCap)
			} else {
				buf = buf[:0]
			}
			buf = MarshalSlice(benchEmpSlice, reg, buf)
		}
	})

	b.Run("Sonic", func(b *testing.B) {
		sample, _ := sonic.Marshal(benchEmpSlice)
		b.SetBytes(int64(len(sample)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = sonic.Marshal(benchEmpSlice)
		}
	})

	b.Run("Standard", func(b *testing.B) {
		sample, _ := json.Marshal(benchEmpSlice)
		b.SetBytes(int64(len(sample)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(benchEmpSlice)
		}
	})

	b.Run("Protobuf", func(b *testing.B) {
		sample, _ := proto.Marshal(pbEmployees)
		b.SetBytes(int64(len(sample)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = proto.Marshal(pbEmployees)
		}
	})
}

// BenchmarkLargeScaleComparison tests deserialization (unmarshal/parsing)
// of a large struct array (100,000 Employee objects) from JSON and Protobuf formats.
// Compares: SilentJSON (with parallel parsing), Sonic in parallel mode,
// Sonic in sequential mode, encoding/json (Standard), and Protobuf (proto.Unmarshal).
func BenchmarkLargeScaleComparison(b *testing.B) {
	// Serialize once for all to exclude it from the benchmark
	pbEmployees := &pb.Employees{
		List: make([]*pb.Employee, len(benchEmpSlice)),
	}

	for i, emp := range benchEmpSlice {
		pbEmployees.List[i] = &pb.Employee{
			Id:       int32(emp.ID),
			IsActive: emp.IsActive,
			Balance:  emp.Balance,
			Address: &pb.Address{
				City: emp.Address.City,
				Zip:  int32(emp.Address.Zip),
			},
			Tags:   emp.Tags,
			Scores: sliceIntToInt64(emp.Scores), // Small conversion int -> int64
		}
	}
	rawPB, _ := proto.Marshal(pbEmployees)
	rawJSON, _ := json.Marshal(benchEmpSlice)

	dst := make([]Employee, len(benchEmpSlice))

	b.Run("SilentJSON", func(b *testing.B) {
		reg := BuildRegistry(reflect.TypeOf(Employee{}))
		b.SetBytes(int64(len(rawJSON)))
		b.ReportAllocs()
		b.ResetTimer()
		buf := make([]byte, len(rawJSON))
		for i := 0; i < b.N; i++ {
			copy(buf, rawJSON)
			_, _ = UnmarshalArrayParallel[Employee](buf, reg, dst)
		}
	})

	b.Run("SonicParallel", func(b *testing.B) {
		b.SetBytes(int64(len(rawJSON)))
		b.ReportAllocs()
		b.ResetTimer()
		buf := make([]byte, len(rawJSON))
		for i := 0; i < b.N; i++ {
			copy(buf, rawJSON)

			chunks := make([]Chunk, len(benchEmpSlice)+1000)
			count, _ := findObjectBoundariesASM(buf, chunks)

			workers := runtime.GOMAXPROCS(0)
			batchSize := (count + workers - 1) / workers
			var wg sync.WaitGroup
			for w := 0; w < workers; w++ {
				start := w * batchSize
				end := start + batchSize
				if end > count {
					end = count
				}
				if start >= count {
					break
				}
				wg.Add(1)
				go func(start, end int) {
					defer wg.Done()
					for idx := start; idx < end; idx++ {
						chunk := chunks[idx]
						_ = sonic.Unmarshal(buf[chunk.Start:chunk.End], &dst[idx])
					}
				}(start, end)
			}
			wg.Wait()
		}
	})

	b.Run("Sonic", func(b *testing.B) {
		b.SetBytes(int64(len(rawJSON)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = sonic.Unmarshal(rawJSON, &dst)
		}
	})

	b.Run("Standard", func(b *testing.B) {
		b.SetBytes(int64(len(rawJSON)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = json.Unmarshal(rawJSON, &dst)
		}
	})

	b.Run("Simdjson_AST", func(b *testing.B) {
		b.SetBytes(int64(len(rawJSON)))
		b.ReportAllocs()
		b.ResetTimer()
		var pj *simdjson.ParsedJson
		for i := 0; i < b.N; i++ {
			pj, _ = simdjson.Parse(rawJSON, pj)
		}
	})

	b.Run("Protobuf", func(b *testing.B) {
		b.SetBytes(int64(len(rawPB)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var e pb.Employees
			_ = proto.Unmarshal(rawPB, &e)
		}
	})
}

// Helper function for type conversion
func sliceIntToInt64(s []int) []int64 {
	res := make([]int64, len(s))
	for i, v := range s {
		res[i] = int64(v)
	}
	return res
}

func BenchmarkStreamComparison(b *testing.B) {
	b.Run("SilentJSON_Stream", func(b *testing.B) {
		reg := BuildRegistry(reflect.TypeOf(Employee{}))
		b.SetBytes(int64(len(hugeJSONData)))
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			r := bytes.NewReader(hugeJSONData)
			dec := NewStreamDecoder[Employee](r, reg)
			var emp Employee
			for {
				err := dec.Decode(&emp)
				if err == io.EOF {
					break
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("Standard_Stream", func(b *testing.B) {
		b.SetBytes(int64(len(hugeJSONData)))
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			r := bytes.NewReader(hugeJSONData)
			dec := json.NewDecoder(r)
			_, err := dec.Token()
			if err != nil {
				b.Fatal(err)
			}
			var emp Employee
			for dec.More() {
				err := dec.Decode(&emp)
				if err != nil {
					b.Fatal(err)
				}
			}
			_, _ = dec.Token() // Read closing bracket
		}
	})
}
