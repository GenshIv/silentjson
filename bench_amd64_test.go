//go:build amd64

package silentjson

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/bytedance/sonic"
)

func BenchmarkSilentJSON_Architecture(b *testing.B) {
	rawJSON, _ := json.Marshal(benchEmpSlice)
	dst := make([]Employee, len(benchEmpSlice))
	reg := BuildRegistry(reflect.TypeOf(Employee{}))

	originalHasAVX2 := hasAVX2
	defer func() { hasAVX2 = originalHasAVX2 }()

	b.Run("AVX2_Parallel", func(b *testing.B) {
		hasAVX2 = true // Force AVX2
		b.SetBytes(int64(len(rawJSON)))
		b.ReportAllocs()
		b.ResetTimer()
		buf := make([]byte, len(rawJSON))
		for i := 0; i < b.N; i++ {
			copy(buf, rawJSON)
			_, _ = UnmarshalArrayParallel[Employee](buf, reg, dst)
		}
	})

	b.Run("Scalar_Parallel", func(b *testing.B) {
		hasAVX2 = false // Force pure Go Scalar
		b.SetBytes(int64(len(rawJSON)))
		b.ReportAllocs()
		b.ResetTimer()
		buf := make([]byte, len(rawJSON))
		for i := 0; i < b.N; i++ {
			copy(buf, rawJSON)
			_, _ = UnmarshalArrayParallel[Employee](buf, reg, dst)
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
}
