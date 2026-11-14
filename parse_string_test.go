package silentjson

// End-to-end differential test for readJSONString against encoding/json.
// The two-phase parser hands work between a zero-copy scanner, an assembly
// block decoder and a Go \u decoder, so the interesting cases are the ones
// that land on a handoff: a backslash on the last byte of a 32-byte block, a
// \uXXXX straddling the boundary, a surrogate pair split across blocks.

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

// checkAgainstEncodingJSON parses lit (a complete JSON string literal,
// quotes included) with both decoders and requires them to agree.
func checkAgainstEncodingJSON(t *testing.T, name, lit string) {
	t.Helper()

	var want string
	wantErr := json.Unmarshal([]byte(lit), &want)

	src := []byte(lit)
	got, content, consumed, err := readJSONString(src[1:], false)

	if wantErr != nil {
		if err == nil {
			t.Errorf("%s: encoding/json rejected %q (%v) but readJSONString returned %q", name, lit, wantErr, got)
		}
		return
	}
	if err != nil {
		t.Errorf("%s: readJSONString(%q) failed: %v", name, lit, err)
		return
	}
	if got != want {
		t.Errorf("%s: readJSONString(%q) = %q, encoding/json = %q", name, lit, got, want)
	}
	if string(content) != want {
		t.Errorf("%s: content = %q, want %q", name, content, want)
	}
	if consumed != int64(len(lit)-1) {
		t.Errorf("%s: consumed = %d, want %d", name, consumed, len(lit)-1)
	}
}

func TestReadJSONStringAgainstEncodingJSON(t *testing.T) {
	type tc struct{ name, lit string }
	cases := []tc{
		{"empty", `""`},
		{"single", `"A"`},
		{"len31", `"` + strings.Repeat("a", 31) + `"`},
		{"len32", `"` + strings.Repeat("a", 32) + `"`},
		{"len33", `"` + strings.Repeat("a", 33) + `"`},
		{"len63", `"` + strings.Repeat("a", 63) + `"`},
		{"len64", `"` + strings.Repeat("a", 64) + `"`},
		{"len65", `"` + strings.Repeat("a", 65) + `"`},

		{"escape only", `"\n"`},
		{"all simple escapes", `"\"\\\/\b\f\n\r\t"`},
		{"escaped quote mid", `"ab\"cd"`},
		{"escaped backslash then end", `"ab\\"`},
		{"escaped backslash then quote escape", `"ab\\\"cd"`},
		{"long escaped backslash run", `"` + strings.Repeat(`\\`, 20) + `"`},
		{"alternating", `"` + strings.Repeat(`a\n`, 30) + `"`},

		{"surrogate pair escape", `"\ud83d\ude00"`},
		{"surrogate pair in text", `"hi \ud83d\ude00 there"`},
		{"unpaired high surrogate", `"\ud83d"`},
		{"unpaired low surrogate", `"\ude00"`},
		{"high surrogate then plain", `"\ud83dA"`},
		{"high surrogate then bmp escape", `"\ud83d\u0041"`},
		{"bmp escape", `"\u00e9"`},
		{"nul escape", `"\u0000"`},
		{"ascii escape", `"\u0041"`},
		{"escape of a backslash via u", `"\u005c"`},

		{"utf8 passthrough", "\"héllo wörld \U0001f600\""},
	}

	// A backslash (and a \u, and a surrogate pair) at every offset around the
	// 32-byte block boundaries.
	for _, pad := range []int{0, 1, 29, 30, 31, 32, 33, 61, 62, 63, 64} {
		p := strings.Repeat("a", pad)
		at := " at " + strconv.Itoa(pad)
		tailB := strings.Repeat("b", 40)
		cases = append(cases,
			tc{"simple escape" + at, `"` + p + `\n` + tailB + `"`},
			tc{"escaped quote" + at, `"` + p + `\"` + tailB + `"`},
			tc{"escaped backslash" + at, `"` + p + `\\` + tailB + `"`},
			tc{"u escape" + at, `"` + p + `\u00e9` + tailB + `"`},
			tc{"surrogate" + at, `"` + p + `\ud83d\ude00` + tailB + `"`},
			tc{"unpaired surrogate" + at, `"` + p + `\ud83d` + tailB + `"`},
			tc{"escape then quote" + at, `"` + p + `\n"`},
			tc{"u escape then quote" + at, `"` + p + `\u00e9"`},
			// Two escapes straddling the boundary from both sides.
			tc{"double escape" + at, `"` + p + `\n\t` + tailB + `"`},
			tc{"escape then u" + at, `"` + p + `\n\u00e9` + tailB + `"`},
		)
	}

	for _, c := range cases {
		checkAgainstEncodingJSON(t, c.name, c.lit)
	}
}

func TestReadJSONStringErrors(t *testing.T) {
	type tc struct{ name, lit string }
	cases := []tc{
		{"unterminated empty", `"`},
		{"unterminated short", `"abc`},
		{"unterminated long", `"` + strings.Repeat("a", 100)},
		{"invalid escape", `"\x"`},
		{"invalid escape after block", `"` + strings.Repeat("a", 40) + `\x"`},
		{"trailing backslash", `"abc\`},
		{"trailing backslash after block", `"` + strings.Repeat("a", 40) + `\`},
		{"truncated u", `"\u00"`},
		{"truncated u at eof", `"\u00`},
		{"bad hex", `"\uZZZZ"`},
		{"bad hex after block", `"` + strings.Repeat("a", 40) + `\uZZZZ"`},
	}
	for _, c := range cases {
		if _, _, _, err := readJSONString([]byte(c.lit)[1:], false); err == nil {
			t.Errorf("%s: readJSONString(%q) succeeded, want error", c.name, c.lit)
		}
		// encoding/json must agree these are not valid strings.
		var s string
		if err := json.Unmarshal([]byte(c.lit), &s); err == nil {
			t.Errorf("%s: encoding/json accepted %q as %q - test case is wrong", c.name, c.lit, s)
		}
	}
}

// TestReadJSONStringZeroCopy pins the phase-1 guarantee: an escape-free string
// is returned as an alias of the input, with nothing copied.
func TestReadJSONStringZeroCopy(t *testing.T) {
	for _, n := range []int{0, 1, 31, 32, 33, 1000} {
		src := []byte(`"` + strings.Repeat("a", n) + `"`)
		body := src[1:]

		allocs := testing.AllocsPerRun(100, func() {
			if _, _, _, err := readJSONString(body, false); err != nil {
				t.Fatal(err)
			}
		})
		if allocs != 0 {
			t.Errorf("len=%d: readJSONString allocated %v times, want 0 (zero-copy fast path)", n, allocs)
		}

		s, content, consumed, err := readJSONString(body, false)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 && &content[0] != &body[0] {
			t.Errorf("len=%d: content is a copy, want an alias of the input", n)
		}
		if s != strings.Repeat("a", n) || consumed != int64(n+1) {
			t.Errorf("len=%d: got %q consumed=%d", n, s, consumed)
		}
	}
}

// TestReadJSONStringNoInputMutation pins that decoding never writes through
// the input slice, which the zero-copy aliasing elsewhere depends on.
func TestReadJSONStringNoInputMutation(t *testing.T) {
	lit := `"` + strings.Repeat("a", 40) + `\n\ud83d\ude00\u0041` + strings.Repeat("b", 40) + `"`
	src := []byte(lit)
	orig := append([]byte(nil), src...)
	if _, _, _, err := readJSONString(src[1:], false); err != nil {
		t.Fatal(err)
	}
	if string(src) != string(orig) {
		t.Errorf("input was mutated:\n got %q\nwant %q", src, orig)
	}
}

// stringTokens are the building blocks of the randomized differential test.
// Mixing single- and multi-byte tokens walks escapes across every alignment
// relative to the 32-byte block grid, which is where the handoffs between the
// scanner, the assembly block decoder and the Go \u decoder live.
var stringTokens = []string{
	"a", "bb", "ccc", " ", "0", "Z", "\u00e9", "\u20ac", "\U0001f600",
	`\n`, `\t`, `\r`, `\b`, `\f`, `\/`, `\"`, `\\`,
	`\u0041`, `\u0000`, `\u00e9`, `\u07ff`, `\uffff`, `\u0022`, `\u005c`,
	`\ud83d`, `\ude00`, `\ud83d\ude00`,
}

func TestReadJSONStringRandomDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5115e7))
	var sb strings.Builder
	for iter := 0; iter < 20000; iter++ {
		sb.Reset()
		sb.WriteByte('"')
		for n := rng.Intn(80); n > 0; n-- {
			sb.WriteString(stringTokens[rng.Intn(len(stringTokens))])
		}
		sb.WriteByte('"')
		lit := sb.String()

		var want string
		if err := json.Unmarshal([]byte(lit), &want); err != nil {
			t.Fatalf("test generator produced invalid JSON %q: %v", lit, err)
		}

		src := []byte(lit)
		orig := append([]byte(nil), src...)
		got, _, consumed, err := readJSONString(src[1:], false)
		if err != nil {
			t.Fatalf("readJSONString(%q) failed: %v", lit, err)
		}
		if got != want {
			t.Fatalf("readJSONString(%q)\n got %q\nwant %q", lit, got, want)
		}
		if consumed != int64(len(lit)-1) {
			t.Fatalf("readJSONString(%q): consumed = %d, want %d", lit, consumed, len(lit)-1)
		}
		if !bytes.Equal(src, orig) {
			t.Fatalf("readJSONString(%q) mutated its input", lit)
		}
	}
}
