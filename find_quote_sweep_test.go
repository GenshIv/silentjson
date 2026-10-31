package silentjson

// Exhaustive equivalence test for the AVX2 string scanner against the
// scalar reference. The scanner runs on every key and string value, so an
// off-by-one here corrupts all parsing; the splitter escape-carry bug was
// exactly this class of positional assembly defect.

import "testing"

func refFindQuoteOrEscape(b []byte) (int, bool) {
	for i, c := range b {
		if c == '"' {
			return i, false
		}
		if c == '\\' {
			return i, true
		}
	}
	return -1, false
}

func TestFindQuoteOrEscapeSweep(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}
	check := func(buf []byte) {
		t.Helper()
		wantIdx, wantEsc := refFindQuoteOrEscape(buf)
		gotIdx, gotEsc := findQuoteOrEscapeAVX2(buf)
		if gotIdx != wantIdx || gotEsc != wantEsc {
			t.Fatalf("len=%d buf=%q: got (%d,%v), want (%d,%v)",
				len(buf), buf, gotIdx, gotEsc, wantIdx, wantEsc)
		}
	}

	// Exhaustive for small lengths: every placement of '"' and '\' (each
	// possibly absent) over filler bytes, covering all block/tail offsets.
	for n := 0; n <= 40; n++ {
		for q := -1; q < n; q++ {
			for e := -1; e < n; e++ {
				buf := make([]byte, n)
				for i := range buf {
					buf[i] = 'a' + byte(i%26)
				}
				if q >= 0 {
					buf[q] = '"'
				}
				if e >= 0 {
					if e == q {
						continue // one byte cannot be both
					}
					buf[e] = '\\'
				}
				check(buf)
			}
		}
	}

	// Long buffers: single special bytes at every offset across several
	// SIMD blocks, including exactly at 32-byte boundaries.
	for n := range []int{64, 96, 129} {
		for pos := 0; pos < n; pos++ {
			for _, spec := range []byte{'"', '\\', 'x'} {
				buf := make([]byte, n)
				for i := range buf {
					buf[i] = 'b'
				}
				if spec != 'x' {
					buf[pos] = spec
				}
				check(buf)
			}
		}
	}

	// Both specials, order-mixing across a boundary.
	for q := 24; q <= 40; q++ {
		for e := 24; e <= 48; e++ {
			if e == q || q >= 64 || e >= 64 {
				continue
			}
			buf := make([]byte, 64)
			for i := range buf {
				buf[i] = 'c'
			}
			buf[q] = '"'
			buf[e] = '\\'
			check(buf)
		}
	}
}
