package silentjson

import (
	"fmt"
	"testing"
)

func TestDebugAsm(t *testing.T) {
	buf := make([]byte, 4096)
	copy(buf, "[1, 42, 999]")
	rawInner := buf[1:12]
	
	var chunks [1]Chunk
	count, _ := findArrayElementsEarlyExitASM(rawInner, chunks[:])
	fmt.Printf("Call 1: Count: %d, End: %d\n", count, chunks[0].End)
	
	fmt.Printf("RawInner: %q\n", rawInner)
	fmt.Printf("Count: %d\n", count)
	for i := 0; i < count; i++ {
		fmt.Printf("Chunk %d: %d to %d -> %q\n", i, chunks[i].Start, chunks[i].End, rawInner[chunks[i].Start:chunks[i].End])
	}
}
