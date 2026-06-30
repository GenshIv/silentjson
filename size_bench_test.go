package silentjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"runtime"

	"github.com/bytedance/sonic"
)

var sizesToTest = []int{5, 100, 1000, 10000, 100000, 1000000, 5000000} // Number of elements

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

		b.Run("Sonic_Parallel_"+namePrefix, func(b *testing.B) {
			b.SetBytes(int64(len(rawJSON)))
			dst := make([]Employee, numElements)
			b.ResetTimer()
			buf := make([]byte, len(rawJSON))
			for i := 0; i < b.N; i++ {
				copy(buf, rawJSON)
				chunks := make([]Chunk, len(dst)+1000)
				count, _ := findArrayElementsEarlyExitASM(buf[1:], chunks) // skip '['
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
							_ = sonic.Unmarshal(buf[1:][chunk.Start:chunk.End], &dst[idx])
						}
					}(start, end)
				}
				wg.Wait()
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
