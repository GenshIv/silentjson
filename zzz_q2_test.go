package silentjson

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"unsafe"
)

type dbgRec struct {
	Str string `json:"str"`
}

func TestDbgN(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(dbgRec{}))
	for _, in := range []string{`{"000":n}`, `{"a":n}`, `{"a":nu}`, `{"a":nul}`, `{"a":nullx}`, `{"a":t}`, `{"a":f}`, `{"a":tru}`, `{"a":null"}`,"{\"a\":null\t}"} {
		var v dbgRec
		sjErr := ParseObject([]byte(in), reg, unsafe.Pointer(&v))
		var w dbgRec
		stdErr := json.Unmarshal([]byte(in), &w)
		fmt.Printf("%-16q sj=%v std=%v\n", in, sjErr, stdErr)
	}
}
