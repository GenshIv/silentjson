package silentjson

// Tolerant-mode tests: damaged input must yield the healthy records plus
// an accurate Problem list — never a wholesale rejection. Strict APIs
// keep failing on the same inputs (their existing tests already pin that).

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

type tolRec struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestTolerantBatch(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(tolRec{}))
	cases := []struct {
		name      string
		input     string
		wantIDs   []int
		wantProbs int
	}{
		{"clean", `[{"id":1},{"id":2}]`, []int{1, 2}, 0},
		{"damaged record in the middle", `[{"id":1},{"id":xx},{"id":3}]`, []int{1, 3}, 1},
		{"leading comma", `[,{"id":1}]`, []int{1}, 1},
		{"doubled comma", `[{"id":1},,{"id":2}]`, []int{1, 2}, 1},
		{"trailing comma", `[{"id":1},]`, []int{1}, 1},
		{"unterminated array", `[{"id":1},{"id":2}`, []int{1, 2}, 1},
		{"truncated tail", `[{"id":1},{"na`, []int{1}, 1},
		{"garbage only", `[{"id":zz}`, nil, 1},
	}
	for _, c := range cases {
		dst := make([]tolRec, 16)
		out, problems := UnmarshalSliceTolerant([]byte(c.input), reg, dst)
		ids := make([]int, len(out))
		for i, r := range out {
			ids[i] = r.ID
		}
		if len(ids) != len(c.wantIDs) {
			t.Errorf("%s: got IDs %v, want %v", c.name, ids, c.wantIDs)
		} else {
			for i := range ids {
				if ids[i] != c.wantIDs[i] {
					t.Errorf("%s: got IDs %v, want %v", c.name, ids, c.wantIDs)
					break
				}
			}
		}
		if len(problems) != c.wantProbs {
			t.Errorf("%s: got %d problems, want %d: %v", c.name, len(problems), c.wantProbs, problems)
		}
	}

	// strict policy over the same machinery: first problem is the error
	dst := make([]tolRec, 16)
	if _, err := UnmarshalSlice([]byte(`[{"id":1},{"id":xx},{"id":3}]`), reg, dst); err == nil {
		t.Error("strict UnmarshalSlice must still fail on the damaged record")
	}
}

func TestTolerantStream(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(tolRec{}))
	cases := []struct {
		name      string
		input     string
		wantIDs   []int
		wantProbs int
	}{
		{"clean", `[{"id":1},{"id":2}]`, []int{1, 2}, 0},
		{"damaged record mid-stream", `[{"id":1},{"id":xx},{"id":3}]`, []int{1, 3}, 1},
		{"leading comma", `[,{"id":1},{"id":2}]`, []int{1, 2}, 1},
		{"trailing comma", `[{"id":1},{"id":2},]`, []int{1, 2}, 1},
		{"unterminated array", `[{"id":1},{"id":2}`, []int{1, 2}, 1},
		{"truncated tail", `[{"id":1},{"id":2},{"na`, []int{1, 2}, 1},
	}
	for _, c := range cases {
		var problems []Problem
		dec := NewStreamDecoder[tolRec](strings.NewReader(c.input), reg).
			Tolerant(func(p Problem) { problems = append(problems, p) })
		var ids []int
		for {
			var v tolRec
			err := dec.Decode(&v)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s: tolerant stream returned error %v", c.name, err)
			}
			ids = append(ids, v.ID)
		}
		if len(ids) != len(c.wantIDs) {
			t.Errorf("%s: got IDs %v, want %v", c.name, ids, c.wantIDs)
		} else {
			for i := range ids {
				if ids[i] != c.wantIDs[i] {
					t.Errorf("%s: got IDs %v, want %v", c.name, ids, c.wantIDs)
					break
				}
			}
		}
		if len(problems) != c.wantProbs {
			t.Errorf("%s: got %d problems, want %d: %v", c.name, len(problems), c.wantProbs, problems)
		}
	}

	// strict stream still fails fast on the same input
	dec := NewStreamDecoder[tolRec](strings.NewReader(`[{"id":1},{"id":xx},{"id":3}]`), reg)
	var v tolRec
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("first record must decode: %v", err)
	}
	if err := dec.Decode(&v); err == nil {
		t.Error("strict stream must fail on the damaged record")
	}
}
