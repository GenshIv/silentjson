package silentjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/bytedance/sonic"
)

var sizesToTest = []int{5, 100, 1000, 10000, 100000} // Number of elements

func BenchmarkSizeScalability(b *testing.B) {
	for _, numElements := range sizesToTest {
		// Generate Data
		items := make([]Employee, numElements)
		for i := 0; i < numElements; i++ {
			items[i] = Employee{
				ID:       i,
				IsActive: true,
				Balance:  123.45,
				Address: Address{
					City: "New York",
					Zip:  10001,
				},
				Tags:   []string{"admin", "user"},
				Scores: []int{10, 20, 30},
			}
		}
		rawJSON, _ := json.Marshal(items)
		mbSize := float64(len(rawJSON)) / (1024.0 * 1024.0)
		namePrefix := fmt.Sprintf("%.2fMB", mbSize)

		b.Run("SilentJSON_Stream_"+namePrefix, func(b *testing.B) {
			reg := BuildRegistry(reflect.TypeOf(Employee{}))
			b.SetBytes(int64(len(rawJSON)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r := bytes.NewReader(rawJSON)
				dec := NewStreamDecoder[Employee](r, reg)
				for {
					_, count, err := dec.NextRawBlock(1000, 0)
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
					if count == 0 {
						break
					}
				}
			}
		})

		b.Run("SilentJSON_Parallel_"+namePrefix, func(b *testing.B) {
			reg := BuildRegistry(reflect.TypeOf(Employee{}))
			b.SetBytes(int64(len(rawJSON)))
			dst := make([]Employee, numElements)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = UnmarshalArrayParallel[Employee](rawJSON, reg, dst)
			}
		})

		b.Run("Sonic_"+namePrefix, func(b *testing.B) {
			b.SetBytes(int64(len(rawJSON)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var dst []Employee
				_ = sonic.Unmarshal(rawJSON, &dst)
			}
		})

		b.Run("Standard_"+namePrefix, func(b *testing.B) {
			b.SetBytes(int64(len(rawJSON)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var dst []Employee
				_ = json.Unmarshal(rawJSON, &dst)
			}
		})
	}
}
