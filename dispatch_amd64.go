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

func findObjectBoundariesEarlyExitASM(data []byte, chunks []Chunk) (int, int) {
	if hasAVX2 {
		return findObjectBoundariesEarlyExitAVX2(data, chunks)
	}
	return findObjectBoundariesEarlyExitScalar(data, chunks)
}

func findArrayElementsEarlyExitASM(data []byte, chunks []Chunk) (int, int) {
	if hasAVX2 {
		return findArrayElementsEarlyExitAVX2(data, chunks)
	}
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

func findQuoteOrEscapeASM(b []byte) (idx int, isEscape bool) {
	if hasAVX2 {
		return findQuoteOrEscapeAVX2(b)
	}
	return findQuoteOrEscapeScalar(b)
}
