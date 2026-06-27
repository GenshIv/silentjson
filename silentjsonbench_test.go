package silentjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"unsafe"

	"github.com/GenshIv/silentjson/pb"
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
	testFileName   = "nested_huge_data.json"
	recordCount    = 3_000_000
	benchSliceSize = 100_000
)

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

func BenchmarkNestedStandard(b *testing.B) {
	b.SetBytes(int64(len(hugeJSONData)))
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
}

func BenchmarkNestedSystem(b *testing.B) {
	reg := BuildRegistry(reflect.TypeOf(Employee{}))
	b.SetBytes(int64(len(hugeJSONData)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var emp Employee
		ptr := unsafe.Pointer(&emp)

		err := UnmarshalSlice(hugeJSONData, reg, ptr)
		if err != nil {
			b.Error(err)
		}
	}
}

func BenchmarkMarshalStandardSlice(b *testing.B) {
	sample, _ := json.Marshal(benchEmpSlice)
	b.SetBytes(int64(len(sample)))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(benchEmpSlice)
	}
}

func BenchmarkMarshalSystemSlice(b *testing.B) {
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
}

func BenchmarkUnmarshalArrayParallel(b *testing.B) {
	// 1. Prepare raw data (source JSON array)
	rawJSON, err := json.Marshal(benchEmpSlice)
	if err != nil {
		b.Fatal(err)
	}

	reg := BuildRegistry(reflect.TypeOf(Employee{}))

	// 2. Set up benchmark metrics
	b.SetBytes(int64(len(rawJSON)))
	b.ReportAllocs()
	b.ResetTimer()

	// 3. Hot loop
	for i := 0; i < b.N; i++ {
		// use clean code
		_, err := UnmarshalArrayParallel[Employee](rawJSON, reg)
		if err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkLargeScaleComparison(b *testing.B) {
	// Сериализуем один раз для всех, чтобы не учитывать это в тесте
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
			Scores: sliceIntToInt64(emp.Scores), // Небольшая конвертация int -> int64
		}
	}
	rawPB, _ := proto.Marshal(pbEmployees)

	b.Run("SilentJSON", func(b *testing.B) {
		reg := BuildRegistry(reflect.TypeOf(Employee{}))
		b.SetBytes(int64(len(rawPB)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Твой параллельный парсер
			_, _ = UnmarshalArrayParallel[Employee](rawPB, reg)
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

// Вспомогательная функция для конвертации типов
func sliceIntToInt64(s []int) []int64 {
	res := make([]int64, len(s))
	for i, v := range s {
		res[i] = int64(v)
	}
	return res
}
