package silentjson

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/GenshIv/silentjson/pb"
	"github.com/bytedance/sonic"
	"github.com/minio/simdjson-go"
	"google.golang.org/protobuf/proto"
)

func BenchmarkScalability(b *testing.B) {
	sizes := []int{10, 50, 100, 500, 1000, 5000, 10000, 25000, 50000, 100000}
	reg := BuildRegistry(reflect.TypeOf(Employee{}))

	for _, size := range sizes {
		if size > len(benchEmpSlice) {
			continue
		}
		subSlice := benchEmpSlice[:size]

		// Protobuf
		pbEmployees := &pb.Employees{
			List: make([]*pb.Employee, size),
		}
		for i, emp := range subSlice {
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

		// SilentJSON
		b.Run(fmt.Sprintf("SilentJSON_%d", size), func(b *testing.B) {
			buf := make([]byte, 0, 1024*1024)
			buf = MarshalSlice(subSlice, reg, buf)
			b.SetBytes(int64(len(buf)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf = buf[:0]
				buf = MarshalSlice(subSlice, reg, buf)
			}
		})

		// Sonic
		b.Run(fmt.Sprintf("Sonic_%d", size), func(b *testing.B) {
			sample, _ := sonic.Marshal(subSlice)
			b.SetBytes(int64(len(sample)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = sonic.Marshal(subSlice)
			}
		})

		// Standard
		b.Run(fmt.Sprintf("Standard_%d", size), func(b *testing.B) {
			sample, _ := json.Marshal(subSlice)
			b.SetBytes(int64(len(sample)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = json.Marshal(subSlice)
			}
		})

		// Protobuf
		b.Run(fmt.Sprintf("Protobuf_%d", size), func(b *testing.B) {
			sample, _ := proto.Marshal(pbEmployees)
			b.SetBytes(int64(len(sample)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = proto.Marshal(pbEmployees)
			}
		})
	}
}

func BenchmarkScalabilityParse(b *testing.B) {
	sizes := []int{10, 50, 100, 500, 1000, 5000, 10000, 25000, 50000, 100000}
	reg := BuildRegistry(reflect.TypeOf(Employee{}))

	for _, size := range sizes {
		if size > len(benchEmpSlice) {
			continue
		}
		subSlice := benchEmpSlice[:size]

		// Prepare JSON & Protobuf payloads
		rawJSON, _ := json.Marshal(subSlice)

		pbEmployees := &pb.Employees{
			List: make([]*pb.Employee, size),
		}
		for i, emp := range subSlice {
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
		rawPB, _ := proto.Marshal(pbEmployees)

		var dst []Employee

		// SilentJSON
		b.Run(fmt.Sprintf("SilentJSON_%d", size), func(b *testing.B) {
			b.SetBytes(int64(len(rawJSON)))
			b.ReportAllocs()
			b.ResetTimer()
			buf := make([]byte, len(rawJSON))
			for i := 0; i < b.N; i++ {
				copy(buf, rawJSON)
				_, _ = UnmarshalArrayParallel[Employee](buf, reg, dst)
			}
		})

		// Sonic
		b.Run(fmt.Sprintf("Sonic_%d", size), func(b *testing.B) {
			b.SetBytes(int64(len(rawJSON)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = sonic.Unmarshal(rawJSON, &dst)
			}
		})

		// Standard
		b.Run(fmt.Sprintf("Standard_%d", size), func(b *testing.B) {
			b.SetBytes(int64(len(rawJSON)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = json.Unmarshal(rawJSON, &dst)
			}
		})

		// Simdjson_AST
		b.Run(fmt.Sprintf("Simdjson_%d", size), func(b *testing.B) {
			b.SetBytes(int64(len(rawJSON)))
			b.ReportAllocs()
			b.ResetTimer()
			var pj *simdjson.ParsedJson
			for i := 0; i < b.N; i++ {
				pj, _ = simdjson.Parse(rawJSON, pj)
			}
		})

		// Protobuf
		b.Run(fmt.Sprintf("Protobuf_%d", size), func(b *testing.B) {
			b.SetBytes(int64(len(rawPB)))
			b.ReportAllocs()
			b.ResetTimer()
			var pbDst pb.Employees
			for i := 0; i < b.N; i++ {
				_ = proto.Unmarshal(rawPB, &pbDst)
			}
		})
	}
}
