// Package benchpayload holds the data types used by the cross-parser
// comparison benchmarks (parser_compare_bench_test.go).
//
// The types live in their own package because easyjson's code generator
// cannot process _test.go files; the generated easyjson code therefore
// sits next to the definitions here. The generator is invoked with
// -no_std_marshalers, so no MarshalJSON/UnmarshalJSON methods are added
// to the types and the other parsers (sonic, goccy, encoding/json,
// silentjson) keep their own code paths — the comparison stays honest.
package benchpayload

//easyjson:json
type Address struct {
	City string `json:"city"`
	Zip  int    `json:"zip"`
}

//easyjson:json
type Employee struct {
	ID       int      `json:"id"`
	IsActive bool     `json:"is_active"`
	Balance  float64  `json:"balance"`
	Address  Address  `json:"address"`
	Tags     []string `json:"tags"`
	Scores   []int    `json:"scores"`
}

// Employees is the root array type for the comparison payload. easyjson
// generates unmarshal methods on named types only, so the root needs a
// named slice to go through the easyjson fast path.
//
//easyjson:json
type Employees []Employee
