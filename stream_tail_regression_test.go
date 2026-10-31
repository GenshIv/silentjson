package silentjson

// Regression tests for stream tail handling. The old nextChunk handed an
// EOF fragment out as a "record" unconditionally — a stream cut in the
// middle of a value delivered malformed JSON ({"id) as a valid chunk. The
// fragment is now validated grammatically: exactly one complete value at
// EOF is delivered (a valid last element cut off from its terminator),
// anything else is a clean truncation error.

import (
	"io"
	"reflect"
	"testing"
)

type tailRec struct {
	ID int `json:"id"`
}

// chunkReader delivers data in fixed-size reads, like a socket: the
// closing bracket regularly lands in a separate Read.
type chunkReader struct {
	data []byte
	pos  int
	step int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := r.step
	if n > len(p) {
		n = len(p)
	}
	if r.pos+n > len(r.data) {
		n = len(r.data) - r.pos
	}
	copy(p, r.data[r.pos:r.pos+n])
	r.pos += n
	return n, nil
}

func drainDecode(dec *StreamDecoder[tailRec]) (ids []int, err error) {
	for {
		var v tailRec
		e := dec.Decode(&v)
		if e == io.EOF {
			return ids, nil
		}
		if e != nil {
			return ids, e
		}
		ids = append(ids, v.ID)
	}
}

func drainRaw(dec *StreamDecoder[tailRec]) (n int, err error) {
	for {
		_, e := dec.NextRaw()
		if e == io.EOF {
			return n, nil
		}
		if e != nil {
			return n, e
		}
		n++
	}
}

func TestStreamTailComplete(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(tailRec{}))
	for name, data := range map[string]string{
		"complete":                          `[{"id":1},{"id":2}]`,
		"trailing whitespace":               "[{\"id\":1},{\"id\":2}]  \n",
		"last element unterminated (valid)": `[{"id":1},{"id":2}`,
	} {
		for _, step := range []int{len(data), 3} { // whole-buffer and socket-like reads
			dec := NewStreamDecoder[tailRec](&chunkReader{data: []byte(data), step: step}, reg)
			ids, err := drainDecode(dec)
			if err != nil || len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
				t.Errorf("%s (step=%d): decode got %v err=%v, want [1 2] nil", name, step, ids, err)
			}
			dec2 := NewStreamDecoder[tailRec](&chunkReader{data: []byte(data), step: step}, reg)
			n, err := drainRaw(dec2)
			if err != nil || n != 2 {
				t.Errorf("%s (step=%d): raw got %d err=%v, want 2 nil", name, step, n, err)
			}
		}
	}
}

func TestStreamTailTruncated(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(tailRec{}))
	for name, data := range map[string]string{
		"cut mid-record": `[{"id":1},{"id"`,
		"cut in string":  `[{"id":1},{"id":2},{"na`,
	} {
		for _, step := range []int{len(data), 3} {
			ids, err := drainDecode(NewStreamDecoder[tailRec](&chunkReader{data: []byte(data), step: step}, reg))
			if err == nil {
				t.Errorf("%s (step=%d): decode expected truncation error, got nil (ids=%v)", name, step, ids)
			}
			if len(ids) == 0 {
				t.Errorf("%s (step=%d): valid prefix records were lost", name, step)
			}
			n, err := drainRaw(NewStreamDecoder[tailRec](&chunkReader{data: []byte(data), step: step}, reg))
			if err == nil {
				t.Errorf("%s (step=%d): raw delivered a malformed fragment as a record (n=%d)", name, step, n)
			}
		}
	}
}
