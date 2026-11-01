package silentjson

// Regression tests for the chunk-boundary splitters (findArrayElements /
// findObjectBoundaries). These guards exist because three real bugs were
// found in the wild:
//
//  1. The scalar splitters started every chunk after the first at the
//     separating comma (off-by-one), so ParseObject saw ",{...}" and
//     failed with "expected object start".
//  2. The scalar splitters ignored the documented "stop when the chunk
//     buffer is full" contract, returning a total count beyond
//     len(chunks); callers (NextRawBlock) then read stale zero chunks.
//  3. The AVX2 assembly splitters mishandled the escape carry: a
//     backslash in the LAST byte of a 32-byte SIMD block sets a carry,
//     but the consumed escaped byte was only removed from the struct and
//     quote masks (not the backslash mask), and the scalar tail (<32
//     remaining bytes) ignored the carry entirely. The in-string parity
//     flipped and whole runs of records were merged into one chunk or
//     silently dropped — json.Marshal output of 100k records lost ~3000.
//
// The escape-carry is now handled correctly in assembly; these tests pin
// both implementations (they run whatever the dispatcher selects, plus
// the AVX2 variant directly on amd64).

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
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

// --- escape-carry regression tests (AVX2 assembly bug #3 above) ---
// The bug was positional: it only fired when a backslash landed on the
// LAST byte of a 32-byte SIMD block, which is why small hand-written
// cases passed while json.Marshal output of large slices lost records.

// splitterUnderTest returns the array splitter the dispatcher selects,
// plus the platform's raw assembly implementation when one is available
// (amd64: AVX2, always tested; arm64: the disabled NEON variant, tested
// only in explicit audit mode).
func splitterUnderTest() map[string]func([]byte, []Chunk) (int, int) {
	fns := map[string]func([]byte, []Chunk) (int, int){
		"dispatch": findArrayElementsEarlyExitASM,
	}
	if fn, ok := archArraySplitter(); ok {
		fns["asm"] = fn
	}
	return fns
}

// runSplitter runs fn over the array body (callers pass it including the
// closing bracket) and returns the chunk count.
func runSplitter(fn func([]byte, []Chunk) (int, int), in string) int {
	inner := append([]byte(in[1:len(in)-1]), ']')
	buf := make([]Chunk, 64)
	count, _ := fn(inner, buf)
	return count
}

// TestSplitterEscapeCarryBoundary sweeps an escape sequence across every
// byte offset of the first two 32-byte SIMD blocks, including the case
// where the backslash is the block's last byte and the escaped character
// lands in the next block or in the scalar tail.
func TestSplitterEscapeCarryBoundary(t *testing.T) {
	type tc struct {
		name string
		// esc replaces the raw bytes just after the padding
		esc    string
		tailFn func(pad int) string // filler after the escape, sized so block1 is full
	}
	forty := strings.Repeat("b", 40)
	for _, c := range []tc{
		{"escaped-quote", `\"`, nil},
		{"escaped-backslash", `\\`, nil},
		{"bs-bs-quote", `\\\"`, nil},
	} {
		for name, fn := range splitterUnderTest() {
			for pad := 0; pad <= 96; pad++ {
				in := `["` + strings.Repeat("a", pad) + c.esc + forty + `","x"]`
				if got := runSplitter(fn, in); got != 2 {
					t.Errorf("%s/%s: pad=%d (escape starts at byte %d, mod32=%d): got %d chunks, want 2",
						name, c.name, pad, pad+2, (pad+2)%32, got)
				}
			}
		}
	}
}

// TestSplitterMarshalDifferential verifies the AVX2 splitter against a
// byte-wise reference scan on the exact payload class that broke it:
// json.Marshal output of a large slice whose strings contain backslash
// and quote characters.
func TestSplitterMarshalDifferential(t *testing.T) {
	raw, err := json.Marshal(benchEmpSlice)
	if err != nil {
		t.Fatal(err)
	}
	want := refSplitterBoundaries(raw[1 : len(raw)-1])
	for name, fn := range splitterUnderTest() {
		buf := make([]Chunk, len(benchEmpSlice)+16)
		count, _ := fn(raw[1:], buf) // body includes the closing bracket
		if count != len(want) {
			t.Errorf("%s: count=%d, want %d (records lost?)", name, count, len(want))
			continue
		}
		for i := 0; i < count; i++ {
			if buf[i].Start != want[i][0] || buf[i].End != want[i][1] {
				t.Fatalf("%s: chunk %d = [%d,%d], want [%d,%d]", name, i, buf[i].Start, buf[i].End, want[i][0], want[i][1])
			}
		}
	}
}

// refSplitterBoundaries is a slow, obviously-correct reference: byte scan
// with full escape pairing, returning element [start,end) ranges of an
// array body (without the enclosing brackets).
func refSplitterBoundaries(inner []byte) [][2]int {
	var out [][2]int
	depth, inStr, esc := 0, false, false
	start := -1
	for j := 0; j < len(inner); j++ {
		c := inner[j]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			if depth == 0 {
				start = j
			}
			depth++
		case '}', ']':
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, [2]int{start, j + 1})
			}
		}
	}
	return out
}

// TestSplitterEmptyArrayWithWhitespace pins the "[ ]" bug: the AVX2
// splitter initialized its chunk start at the buffer beginning instead of
// the first non-whitespace byte, so an empty array with inner whitespace
// produced a phantom non-empty chunk (leading space .. ']') that callers
// parsed as a broken element — UnmarshalSlice("[ ]") failed with
// "insufficient capacity: need 1" or "expected object start".
func TestSplitterEmptyArrayWithWhitespace(t *testing.T) {
	for _, in := range []string{`[]`, `[ ]`, "[\t]", "[\r\n ]", `[ ,]`, `[ {"id":1} ]`} {
		inner := append([]byte(in[1:len(in)-1]), ']')
		want := refSplitterBoundaries(inner)
		for name, fn := range splitterUnderTest() {
			buf := make([]Chunk, 16)
			count, _ := fn(inner, buf)
			// every emitted chunk must either be empty (phantom, filtered
			// by callers) or start at a non-whitespace byte
			for i := 0; i < count && i < len(buf); i++ {
				s, e := buf[i].Start, buf[i].End
				if s == e {
					continue
				}
				if inner[s] == ' ' || inner[s] == '\t' || inner[s] == '\n' || inner[s] == '\r' {
					t.Errorf("%s: %q chunk[%d] starts at whitespace byte %q", name, in, i, inner[s])
				}
				_ = e
			}
			_ = want
		}
	}

	reg := BuildRegistry(reflect.TypeOf(splitterRec{}))
	for _, in := range []string{`[]`, `[ ]`, "[\t]", "[\r\n ]", `[ {"id":1} ]`} {
		dst := make([]splitterRec, 8)
		out, err := UnmarshalSlice([]byte(in), reg, dst)
		if err != nil {
			t.Errorf("UnmarshalSlice(%q): %v", in, err)
			continue
		}
		wantN := 0
		if in != `[]` && in != `[ ]` && in != "[\t]" && in != "[\r\n ]" {
			wantN = 1
		}
		if len(out) != wantN {
			t.Errorf("UnmarshalSlice(%q): got %d records, want %d", in, len(out), wantN)
		}
		// and the parallel path agrees
		dst2 := make([]splitterRec, 8)
		out2, err2 := UnmarshalArrayParallel[splitterRec]([]byte(in), reg, dst2)
		if err2 != nil || len(out2) != wantN {
			t.Errorf("UnmarshalArrayParallel(%q): %d records, err=%v", in, len(out2), err2)
		}
	}

	// stream decoder over the same inputs (nextChunk shares the splitter)
	for _, in := range []string{`[]`, `[ ]`} {
		dec := NewStreamDecoder[splitterRec](bytes.NewReader([]byte(in)), reg)
		n := 0
		for {
			var v splitterRec
			err := dec.Decode(&v)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("stream Decode over %q: %v", in, err)
				break
			}
			n++
		}
		if n != 0 {
			t.Errorf("stream Decode over %q: got %d records, want 0", in, n)
		}
	}
}

// TestSplitterWindowLoop pins the scan-window loop in UnmarshalSlice: the
// splitter stops when the chunk buffer fills ("early exit" contract), and a
// single scan silently returned only the first window's records — 500k tiny
// records against a ~157k-chunk estimate parsed just 31% of the payload
// (the README "small records" numbers were ~3x overstated because of it).
// The parser must consume the WHOLE array, windows or not.
func TestSplitterWindowLoop(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(splitterRec{}))
	// 200k records of ~40 bytes = ~8 MB -> chunk estimate len/128+1024 ≈ 63k
	// < 200k: forces multiple scan windows.
	const n = 200_000
	src := make([]splitterRec, n)
	var buf bytes.Buffer
	buf.WriteString("[")
	for i := 0; i < n; i++ {
		src[i].ID = i
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(`{"id":`)
		buf.WriteString(strconv.Itoa(i))
		buf.WriteString(`,"path":"p`)
		buf.WriteString(strconv.Itoa(i % 97))
		buf.WriteString(`"}`)
	}
	buf.WriteString("]")
	raw := buf.Bytes()
	if est := estimateChunkCapacity(raw); est >= n {
		t.Fatalf("test setup: chunk estimate %d >= %d records; make records smaller", est, n)
	}

	dst := make([]splitterRec, n)
	out, err := UnmarshalSlice(raw, reg, dst)
	if err != nil {
		t.Fatalf("UnmarshalSlice: %v", err)
	}
	if len(out) != n {
		t.Fatalf("got %d records, want %d (silent truncation across scan windows?)", len(out), n)
	}
	for i := range out {
		if out[i].ID != i {
			t.Fatalf("record %d: got ID %d", i, out[i].ID)
		}
	}

	// tolerant path agrees on clean data
	out2, problems := UnmarshalSliceTolerant(raw, reg, dst)
	if len(problems) != 0 || len(out2) != n {
		t.Fatalf("tolerant: %d records, %d problems", len(out2), len(problems))
	}
}
