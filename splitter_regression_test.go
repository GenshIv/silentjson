package silentjson

// Regression tests for the chunk-boundary splitters (findArrayElements /
// findObjectBoundaries). These guards exist because two real bugs were
// found in the wild:
//
//  1. The scalar splitters started every chunk after the first at the
//     separating comma (off-by-one), so ParseObject saw ",{...}" and
//     failed with "expected object start".
//  2. The AVX2 assembly splitters mis-tracked escape sequences: a '\\'
//     immediately followed by '\"' desynchronized the string scanner and
//     whole runs of records were merged into one chunk or dropped
//     entirely. UnmarshalSlice on json.Marshal output containing such
//     strings returned ~3% fewer records (pre-strictness: silently).
//
// The AVX2 splitters are disabled in dispatch_amd64.go until the assembly
// is fixed; these tests run against whatever implementation is active, so
// re-enabling a broken one fails here immediately.

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"testing"
)

type splitterRec struct {
	ID    int      `json:"id"`
	Path  string   `json:"path"`
	Regex string   `json:"regex"`
	Tags  []string `json:"tags"`
	Score int      `json:"score"`
}

func buildSplitterPayload(t *testing.T, n int) ([]byte, []splitterRec) {
	t.Helper()
	src := make([]splitterRec, n)
	for i := range src {
		// Strings whose json.Marshal encoding contains \\ followed by \"
		// (backslash+quote in the Go string) — the exact pattern that
		// desynchronized the AVX2 string scanner.
		src[i] = splitterRec{
			ID:    i,
			Path:  `C:\dir\"quoted"\file` + itoa(i),
			Regex: `^\d+\s*\".*\"$`,
			Tags:  []string{`a\"b`, "plain", `c\\d`},
			Score: i % 100,
		}
	}
	raw, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw, src
}

func itoa(i int) string {
	return string([]byte(jsonIntBytes(i)))
}

func jsonIntBytes(i int) []byte {
	b, _ := json.Marshal(i)
	return b
}

// TestSplitterMarshalRoundTrip drives every array-consuming API over a
// json.Marshal-ed payload full of backslash-quote escapes and requires a
// field-exact round trip.
func TestSplitterMarshalRoundTrip(t *testing.T) {
	raw, src := buildSplitterPayload(t, 5000)
	reg := BuildRegistry(reflect.TypeOf(splitterRec{}))

	// Serial splitter path.
	dst := make([]splitterRec, len(src))
	out, err := UnmarshalSlice(raw, reg, dst)
	if err != nil {
		t.Fatalf("UnmarshalSlice: %v", err)
	}
	assertSplitterEqual(t, "UnmarshalSlice", out, src)

	// Parallel splitter path.
	dst2 := make([]splitterRec, len(src))
	out2, err := UnmarshalArrayParallel[splitterRec](raw, reg, dst2)
	if err != nil {
		t.Fatalf("UnmarshalArrayParallel: %v", err)
	}
	assertSplitterEqual(t, "UnmarshalArrayParallel", out2, src)

	// Streaming paths (Decode and Next share nextChunk; NextRawBlock uses
	// the block splitter).
	dec := NewStreamDecoder[splitterRec](bytes.NewReader(raw), reg)
	n := 0
	for {
		var rec splitterRec
		err := dec.Decode(&rec)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream Decode record %d: %v", n, err)
		}
		if !reflect.DeepEqual(rec, src[n]) {
			t.Fatalf("stream Decode record %d: got %+v want %+v", n, rec, src[n])
		}
		n++
	}
	if n != len(src) {
		t.Fatalf("stream Decode: got %d records, want %d", n, len(src))
	}

	dec2 := NewStreamDecoder[splitterRec](bytes.NewReader(raw), reg)
	total := 0
	for {
		block, count, err := dec2.NextRawBlock(64, 0)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextRawBlock: %v", err)
		}
		if count == 0 {
			break
		}
		// Block = comma-separated raw elements without the enclosing
		// brackets (documented contract); wrap and validate via encoding/json.
		wrapped := append(append([]byte{'['}, block...), ']')
		var recs []splitterRec
		if err := json.Unmarshal(wrapped, &recs); err != nil {
			t.Fatalf("NextRawBlock produced invalid JSON: %v (block=%q...)", err, block[:min(60, len(block))])
		}
		total += len(recs)
	}
	if total != len(src) {
		t.Fatalf("NextRawBlock: %d records total, want %d", total, len(src))
	}
}

func assertSplitterEqual(t *testing.T, via string, got, want []splitterRec) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d records, want %d (records lost by splitter?)", via, len(got), len(want))
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("%s: record %d mismatch:\n got %+v\nwant %+v", via, i, got[i], want[i])
		}
	}
}

// TestSplitterNoLeadingComma pins the scalar off-by-one fix: chunk starts
// must point at '{', not at the separating comma. Input mirrors what the
// callers pass: the array body INCLUDING the closing bracket (the final
// chunk is emitted when the scanner sees it).
func TestSplitterNoLeadingComma(t *testing.T) {
	inner := []byte(`{"a":1},{"a":2},{"a":3}]`)
	buf := make([]Chunk, 16)
	count, _ := findArrayElementsEarlyExitASM(inner, buf)
	if count != 3 {
		t.Fatalf("count=%d, want 3", count)
	}
	// Chunks are half-open [Start, End): End points at the separating
	// comma (or the closing bracket for the final chunk) and must not be
	// included; Start must point at '{', never at the comma.
	for i := 0; i < count; i++ {
		if inner[buf[i].Start] != '{' {
			t.Fatalf("chunk[%d] starts with %q (leading comma leaked into chunk)", i, inner[buf[i].Start])
		}
		if inner[buf[i].End-1] != '}' {
			t.Fatalf("chunk[%d] content ends with %q, want '}'", i, inner[buf[i].End-1])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
