package silentjson

// Regression tests for stream tail handling. The old nextChunk handed an
// EOF fragment out as a "record" unconditionally — a stream cut in the
// middle of a value delivered malformed JSON ({"id) as a valid chunk. The
// fragment is now validated grammatically: exactly one complete value at
// EOF is delivered (a valid last element cut off from its terminator),
// anything else is a clean truncation error.
//
// The truncated-envelope fix makes the tail LOUD as well: a stream that
// reaches EOF without ever seeing ']' reports ErrUnexpectedEOF instead of
// ending cleanly. The old behavior made a truncated feed indistinguishable
// from a complete one — and contradicted UnmarshalSlice, which rejects the
// same input (and encoding/json, which reports "unexpected end of JSON
// input"). No data is lost: the healthy records — including a final
// element that is complete but lost its terminator — are delivered FIRST,
// the truncation is reported on the next call (io.Reader-style two-step),
// and the error is sticky.

import (
	"errors"
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
	if n <= 0 {
		n = 1
	}
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

func drainRaw(dec *StreamDecoder[tailRec]) (n int, lastFrag []byte, err error) {
	for {
		chunk, e := dec.NextRaw()
		if e == io.EOF {
			return n, nil, nil
		}
		if e != nil {
			// io.Reader semantics: a truncated stream delivers the partial
			// fragment together with the error.
			return n, chunk, e
		}
		n++
	}
}

func TestStreamTailComplete(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(tailRec{}))
	for name, data := range map[string]string{
		"complete":            `[{"id":1},{"id":2}]`,
		"trailing whitespace": "[{\"id\":1},{\"id\":2}]  \n",
	} {
		for _, step := range []int{len(data), 3} { // whole-buffer and socket-like reads
			dec := NewStreamDecoder[tailRec](&chunkReader{data: []byte(data), step: step}, reg)
			ids, err := drainDecode(dec)
			if err != nil || len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
				t.Errorf("%s (step=%d): decode got %v err=%v, want [1 2] nil", name, step, ids, err)
			}
			dec2 := NewStreamDecoder[tailRec](&chunkReader{data: []byte(data), step: step}, reg)
			n, frag, err := drainRaw(dec2)
			if err != nil || n != 2 || len(frag) != 0 {
				t.Errorf("%s (step=%d): raw got %d frag=%q err=%v, want 2 records, no fragment, nil", name, step, n, frag, err)
			}
		}
	}
}

// TestStreamTailUnterminatedReported pins the loud-truncation policy for
// every shape of a truncated envelope: the stream must report
// ErrUnexpectedEOF (not end with a clean io.EOF), keep every healthy
// record — including the final element that is complete but lost its
// terminator — and keep reporting the error on repeated reads.
func TestStreamTailUnterminatedReported(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(tailRec{}))
	cases := []struct {
		name    string
		input   string
		wantIDs []int
	}{
		{"last element unterminated", `[{"id":1},{"id":2}`, []int{1, 2}},
		{"trailing comma at EOF", `[{"id":1},`, []int{1}},
		{"bare [", `[`, nil},
		{"whitespace body", "[ ", nil},
		{"empty input", ``, nil},
		{"whitespace input", " \n\t", nil},
	}
	for _, c := range cases {
		for _, step := range []int{len(c.input), 3} {
			// Decode: records first, then ErrUnexpectedEOF, sticky.
			dec := NewStreamDecoder[tailRec](&chunkReader{data: []byte(c.input), step: step}, reg)
			var ids []int
			var err error
			for {
				var v tailRec
				if err = dec.Decode(&v); err != nil {
					break
				}
				ids = append(ids, v.ID)
			}
			if !errors.Is(err, ErrUnexpectedEOF) {
				t.Errorf("%s (step=%d): Decode ended with %v, want ErrUnexpectedEOF (truncated envelope must be reported)", c.name, step, err)
			}
			if !reflect.DeepEqual(ids, c.wantIDs) {
				t.Errorf("%s (step=%d): Decode delivered %v, want %v (healthy records must not be lost)", c.name, step, ids, c.wantIDs)
			}
			if err2 := dec.Decode(&tailRec{}); !errors.Is(err2, ErrUnexpectedEOF) {
				t.Errorf("%s (step=%d): repeated Decode after truncation returned %v, want sticky ErrUnexpectedEOF", c.name, step, err2)
			}

			// NextRaw: same policy — records, then the error.
			decRaw := NewStreamDecoder[tailRec](&chunkReader{data: []byte(c.input), step: step}, reg)
			n := 0
			for {
				_, err = decRaw.NextRaw()
				if err != nil {
					break
				}
				n++
			}
			if !errors.Is(err, ErrUnexpectedEOF) || n != len(c.wantIDs) {
				t.Errorf("%s (step=%d): NextRaw delivered %d records, err=%v; want %d + ErrUnexpectedEOF", c.name, step, n, err, len(c.wantIDs))
			}

			// NextRawBlock: salvages the final terminator-less element,
			// then reports the truncation.
			decBlk := NewStreamDecoder[tailRec](&chunkReader{data: []byte(c.input), step: step}, reg)
			nb := 0
			var count int
			for {
				_, count, err = decBlk.NextRawBlock(0, 0)
				if err != nil {
					break
				}
				nb += count
			}
			if !errors.Is(err, ErrUnexpectedEOF) || nb != len(c.wantIDs) {
				t.Errorf("%s (step=%d): NextRawBlock delivered %d records, err=%v; want %d + ErrUnexpectedEOF", c.name, step, nb, err, len(c.wantIDs))
			}

			// NextChan: items, then exactly one error result.
			var nc int
			var chanErr error
			for res := range NewStreamDecoder[tailRec](&chunkReader{data: []byte(c.input), step: step}, reg).NextChan(4) {
				if res.Err != nil {
					chanErr = res.Err
				} else {
					nc++
				}
			}
			if !errors.Is(chanErr, ErrUnexpectedEOF) || nc != len(c.wantIDs) {
				t.Errorf("%s (step=%d): NextChan delivered %d records, err=%v; want %d + ErrUnexpectedEOF", c.name, step, nc, chanErr, len(c.wantIDs))
			}

			// NextChanParallel: the salvaged terminator-less batch must
			// still become a record (worker fallback), then the error.
			var np int
			var parErr error
			for res := range NewStreamDecoder[tailRec](&chunkReader{data: []byte(c.input), step: step}, reg).NextChanParallel(2, 4) {
				if res.Err != nil {
					parErr = res.Err
				} else {
					np++
				}
			}
			if !errors.Is(parErr, ErrUnexpectedEOF) || np != len(c.wantIDs) {
				t.Errorf("%s (step=%d): NextChanParallel delivered %d records, err=%v; want %d + ErrUnexpectedEOF", c.name, step, np, parErr, len(c.wantIDs))
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
			dec := NewStreamDecoder[tailRec](&chunkReader{data: []byte(data), step: step}, reg)
			ids, err := drainDecode(dec)
			if err == nil {
				t.Errorf("%s (step=%d): decode expected truncation error, got nil (ids=%v)", name, step, ids)
			}
			if len(ids) == 0 {
				t.Errorf("%s (step=%d): valid prefix records were lost (ids=%v)", name, step, ids)
			}
			dec2 := NewStreamDecoder[tailRec](&chunkReader{data: []byte(data), step: step}, reg)
			n, frag, err := drainRaw(dec2)
			if err == nil {
				t.Errorf("%s (step=%d): truncation not reported (n=%d)", name, step, n)
			}
			// the partial fragment must still be delivered alongside the error
			if len(frag) == 0 {
				t.Errorf("%s (step=%d): truncated tail fragment lost (io.Reader semantics: bytes + error)", name, step)
			}
			// the truncation is sticky: repeated reads keep reporting it
			if _, err := dec2.NextRaw(); !errors.Is(err, ErrUnexpectedEOF) {
				t.Errorf("%s (step=%d): repeated NextRaw after truncation returned %v, want ErrUnexpectedEOF", name, step, err)
			}
		}
	}
}

// TestStreamArraySeparators pins the separator grammar in the streaming
// paths: leading ([,{}]), doubled ([{},,{}]) and trailing ([{},]) commas
// were silently skipped (and NextRawBlock even counted the phantom chunks
// as records). Only a single comma between two delivered elements is legal.
func TestStreamArraySeparators(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(tailRec{}))
	valid := map[string]int{
		`[]`:                      0,
		`[ ]`:                     0,
		`[{"id":1}]`:              1,
		`[{"id":1},{"id":2}]`:     2,
		`[ {"id":1} , {"id":2} ]`: 2,
	}
	invalid := map[string]string{
		`[,{}]`:                "leading comma",
		`[{"id":1},]`:          "trailing comma",
		`[{"id":1},,{"id":2}]`: "doubled comma",
		`[ ,{"id":1}]`:         "leading comma",
	}

	for in, wantN := range valid {
		ids, err := drainDecode(NewStreamDecoder[tailRec](&chunkReader{data: []byte(in), step: 3}, reg))
		if err != nil || len(ids) != wantN {
			t.Errorf("%q: decode got %d records err=%v, want %d nil", in, len(ids), err, wantN)
		}
		n, _, err := drainRaw(NewStreamDecoder[tailRec](&chunkReader{data: []byte(in), step: 4}, reg))
		if err != nil || n != wantN {
			t.Errorf("%q: raw got %d records err=%v, want %d nil", in, n, err, wantN)
		}
	}

	for in, why := range invalid {
		if _, err := drainDecode(NewStreamDecoder[tailRec](&chunkReader{data: []byte(in), step: 3}, reg)); err == nil {
			t.Errorf("%q (%s): decode accepted malformed separators", in, why)
		}
		if _, _, err := drainRaw(NewStreamDecoder[tailRec](&chunkReader{data: []byte(in), step: 4}, reg)); err == nil {
			t.Errorf("%q (%s): NextRaw accepted malformed separators", in, why)
		}
	}
}
