package silentjson

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

func TestStreamDebugAsm(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(TestWorkerItem{}))
	payload := []byte("[1, 42, 999]")
	
	dec := NewStreamDecoder[TestWorkerItem](bytes.NewReader(payload), reg)
	
	raw, err := dec.NextRaw()
	fmt.Printf("Item 0: err=%v, raw=%q\n", err, raw)
	
	raw2, err2 := dec.NextRaw()
	fmt.Printf("Item 1: err=%v, raw=%q\n", err2, raw2)
}
