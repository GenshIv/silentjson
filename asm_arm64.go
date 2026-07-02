//go:build arm64

package silentjson

import "bytes"

//go:noescape
func findQuoteAsm(data []byte) int

func scanJSONStringASM(src []byte) (int, bool) {
	hasEscape := false
	for i := 0; i < len(src); i++ {
		if src[i] == '\\' {
			hasEscape = true
			i++
		} else if src[i] == '"' {
			return i, hasEscape
		}
	}
	return -1, hasEscape
}

func parseShortStringASM(src []byte) ([]byte, int64) {
	w, c := parseShortStringASM2(src)
	if c < 0 {
		return nil, -1
	}
	return src[:w], c
}

func parseShortStringASM2(src []byte) (int64, int64) {
	end, hasEscape := scanJSONStringASM(src)
	if end < 0 {
		return 0, -1
	}
	if !hasEscape {
		return int64(end), int64(end + 1)
	}
	writeIdx := 0
	for readIdx := 0; readIdx < end; readIdx++ {
		if src[readIdx] == '\\' && readIdx+1 < end {
			readIdx++
			switch src[readIdx] {
			case 'n':
				src[writeIdx] = '\n'
			case '"':
				src[writeIdx] = '"'
			case '\\':
				src[writeIdx] = '\\'
			case 'r':
				src[writeIdx] = '\r'
			case 't':
				src[writeIdx] = '\t'
			case '/':
				src[writeIdx] = '/'
			case 'b':
				src[writeIdx] = '\b'
			case 'f':
				src[writeIdx] = '\f'
			default:
				src[writeIdx] = src[readIdx]
			}
			writeIdx++
		} else {
			src[writeIdx] = src[readIdx]
			writeIdx++
		}
	}
	return int64(writeIdx), int64(end + 1)
}

func appendIntASM(buf []byte, val int64) []byte {
	var b [20]byte
	var i = 19
	var neg = val < 0
	if neg {
		val = -val
	}
	for val >= 10 {
		q := val / 10
		b[i] = byte('0' + val - q*10)
		i--
		val = q
	}
	b[i] = byte('0' + val)
	if neg {
		i--
		b[i] = '-'
	}
	return append(buf, b[i:]...)
}

func appendStringASM(buf []byte, s string) ([]byte, int) {
	buf = append(buf, '"')
	specialPos := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 {
			specialPos = i
			break
		}
		buf = append(buf, c)
	}
	if specialPos == -1 {
		buf = append(buf, '"')
	}
	return buf, specialPos
}

//go:noescape
func skipSpaceASM(data []byte, start int) int

func skipValueASM(raw []byte, start int) int {
	return skipValue(raw, start)
}

func findQuoteOrEscapeASM(b []byte) (int, bool) {
	idx := bytes.IndexAny(b, "\"\\")
	if idx == -1 {
		return -1, false
	}
	return idx, b[idx] == '\\'
}

//go:noescape
func findObjectBoundariesASM(data []byte, chunks []Chunk) (int, int)

//go:noescape
func findObjectBoundariesEarlyExitASM(data []byte, chunks []Chunk) (int, int)

//go:noescape
func findArrayElementsEarlyExitASM(data []byte, chunks []Chunk) (int, int)
