//go:build amd64

package silentjson

import (
	"golang.org/x/sys/cpu"
)

var hasAVX2 = cpu.X86.HasAVX2

func parseShortStringASM(src []byte) ([]byte, int64) {
	if hasAVX2 {
		return parseShortStringAVX2(src)
	}
	return parseShortStringScalar(src)
}

func parseShortStringASM2(src []byte) (int64, int64) {
	if hasAVX2 {
		return parseShortStringAVX2_2(src)
	}
	return parseShortStringScalar2(src)
}

func findQuoteAsm(data []byte) (index int) {
	if hasAVX2 {
		return findQuoteAVX2(data)
	}
	return findQuoteScalar(data)
}

func appendIntASM(buf []byte, val int64) []byte {
	if hasAVX2 {
		return appendIntAVX2(buf, val)
	}
	return appendIntScalar(buf, val)
}

func appendStringASM(buf []byte, s string) ([]byte, int) {
	if hasAVX2 {
		return appendStringAVX2(buf, s)
	}
	return appendStringScalar(buf, s)
}

func findObjectBoundariesASM(data []byte, chunks []Chunk) (ret0 int, ret1 int) {
	if hasAVX2 {
		return findObjectBoundariesAVX2(data, chunks)
	}
	return findObjectBoundariesScalar(data, chunks)
}

// The two *EarlyExit splitters below deliberately bypass their AVX2
// implementations. The assembly versions mishandle escape sequences:
// a '\\' immediately followed by '\"' (e.g. a Windows path or any
// json.Marshal-ed string containing backslash+quote) desynchronizes the
// string scanner, and whole runs of records are merged into a single
// chunk or silently dropped (repro: json.Marshal of a slice whose
// strings contain `\"` — see TestSplitterMarshalRoundTrip). Until the
// assembly escape handling is fixed, correctness wins over scan speed:
// the fixed scalar splitters are always used. The scalar versions had
// an off-by-one (chunks started at the separating comma) that is also
// fixed and covered by the same tests.
func findObjectBoundariesEarlyExitASM(data []byte, chunks []Chunk) (int, int) {
	return findObjectBoundariesEarlyExitScalar(data, chunks)
}

func findArrayElementsEarlyExitASM(data []byte, chunks []Chunk) (int, int) {
	return findArrayElementsEarlyExitScalar(data, chunks)
}

func skipValueASM(raw []byte, start int) int {
	if hasAVX2 {
		return skipValueAVX2(raw, start)
	}
	return skipValueScalar(raw, start)
}

func skipSpaceASM(data []byte, start int) int {
	if hasAVX2 {
		return skipSpaceAVX2(data, start)
	}
	return skipSpaceScalar(data, start)
}

// findQuoteOrEscapeASM finds the first '"' or '\\' in b.
// NOTE: no AVX2 implementation exists for this one yet (the symbol declared
// in asm.go was never implemented), so we always use the scalar fallback.
func findQuoteOrEscapeASM(b []byte) (idx int, isEscape bool) {
	return findQuoteOrEscapeScalar(b)
}
