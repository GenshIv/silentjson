//go:build amd64

package silentjson

// Differential sweep for the AVX2 string-block decoder against the scalar
// reference. The routine both copies bytes and rewrites escapes, so a
// positional defect here silently corrupts every escaped string in a payload
// instead of failing loudly — the same class of bug the splitter escape-carry
// sweep exists to catch.

import (
	"bytes"
	"math/rand"
	"testing"
	"unsafe"
)

// dstGuard is a 64-byte output buffer (the contract minimum) followed by a
// canary region that must never be touched.
const dstGuardCanary = 0x5a
const dstGuardTail = 32

func runBlock(t *testing.T, name string, fn func(src, dst *byte) (int, int, int), src []byte) (int, int, int, []byte) {
	t.Helper()
	buf := make([]byte, 64+dstGuardTail)
	for i := range buf {
		buf[i] = dstGuardCanary
	}
	c, w, s := fn(unsafe.SliceData(src), unsafe.SliceData(buf))
	for i := 64; i < len(buf); i++ {
		if buf[i] != dstGuardCanary {
			t.Fatalf("%s wrote past the 64-byte dst contract at +%d (src=%q)", name, i, src)
		}
	}
	if w < 0 || w > 32 {
		t.Fatalf("%s returned written=%d out of range (src=%q)", name, w, src)
	}
	return c, w, s, buf[:w]
}

func checkBlockEquivalence(t *testing.T, src []byte) {
	t.Helper()
	gotC, gotW, gotS, gotDst := runBlock(t, "AVX2", parseStringBlockAVX2, src)
	wantC, wantW, wantS, wantDst := runBlock(t, "scalar", parseStringBlockScalar, src)
	if gotC != wantC || gotW != wantW || gotS != wantS || !bytes.Equal(gotDst, wantDst) {
		t.Fatalf("block mismatch for %q:\n AVX2:   consumed=%d written=%d status=%d dst=%q\n scalar: consumed=%d written=%d status=%d dst=%q",
			src, gotC, gotW, gotS, gotDst, wantC, wantW, wantS, wantDst)
	}
}

// blockAlphabet is weighted towards '"' and '\\' so that random blocks are
// dense in terminators and escapes instead of mostly plain text.
var blockAlphabet = []byte{
	'"', '"', '\\', '\\', '\\', '\\',
	'a', 'b', 'f', 'n', 'r', 't', '/', 'u', 'x',
	'0', '1', '9', 'A', 'F', ' ',
	0xc3, 0xa9, 0xf0, 0x9f, 0x98, 0x80, 0x00, 0x1f, 0xff,
}

func TestParseStringBlockSweepRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(20261114))
	src := make([]byte, 32)
	for iter := 0; iter < 200000; iter++ {
		for i := range src {
			src[i] = blockAlphabet[rng.Intn(len(blockAlphabet))]
		}
		checkBlockEquivalence(t, src)
	}
}

// TestParseStringBlockSweepPositional walks every special byte through every
// position in the block, including the backslash-at-byte-31 split case and
// every possible byte following a backslash.
func TestParseStringBlockSweepPositional(t *testing.T) {
	src := make([]byte, 32)
	for pos := 0; pos < 32; pos++ {
		for b := 0; b < 256; b++ {
			for i := range src {
				src[i] = 'a'
			}
			src[pos] = byte(b)
			checkBlockEquivalence(t, src)

			// Same byte, but preceded by a backslash (escape decoding).
			if pos > 0 {
				src[pos-1] = '\\'
				checkBlockEquivalence(t, src)
			}

			// A backslash at pos with b as its payload, plus a quote right
			// after, to pin the "escaped terminator must not terminate" case.
			for i := range src {
				src[i] = 'a'
			}
			src[pos] = '\\'
			if pos+1 < 32 {
				src[pos+1] = byte(b)
			}
			if pos+2 < 32 {
				src[pos+2] = '"'
			}
			checkBlockEquivalence(t, src)
		}
	}
}

// TestParseStringBlockEscTable pins the assembly escape table against the Go
// one: the assembly copy is 32 hand-written quadwords and a transposed nibble
// would be invisible in any single-escape test.
func TestParseStringBlockEscTable(t *testing.T) {
	src := make([]byte, 32)
	for b := 0; b < 256; b++ {
		for i := range src {
			src[i] = 'a'
		}
		src[0] = '\\'
		src[1] = byte(b)
		c, w, s, dst := runBlock(t, "AVX2", parseStringBlockAVX2, src)
		want := escTable[b]
		if want == 0 {
			if s != blockDefer || c != 0 || w != 0 {
				t.Fatalf("\\%#02x: want defer at 0, got consumed=%d written=%d status=%d", b, c, w, s)
			}
			continue
		}
		if s != blockEOB || c != 32 || w != 31 {
			t.Fatalf("\\%#02x: want EOB consumed=32 written=31, got consumed=%d written=%d status=%d", b, c, w, s)
		}
		if dst[0] != want {
			t.Fatalf("\\%#02x: assembly decoded to %#02x, escTable says %#02x", b, dst[0], want)
		}
	}
}
