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

// SmallRec is the "many tiny records" benchmark shape: three fields,
// ~40 bytes per record.
//
//easyjson:json
type SmallRec struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
	OK   bool   `json:"ok"`
}

// SmallRecs is the root array type for easyjson (methods are generated
// on named types only).
//
//easyjson:json
type SmallRecs []SmallRec

// LargeRec is the "few large / very large records" benchmark shape: the
// same field set as Employee plus kilobyte-scale strings and big arrays;
// record size is controlled by the generator.
//
//easyjson:json
type LargeRec struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Scores      []int    `json:"scores"`
	Address     Address  `json:"address"`
	Active      bool     `json:"active"`
	Balance     float64  `json:"balance"`
}

//easyjson:json
type LargeRecs []LargeRec
