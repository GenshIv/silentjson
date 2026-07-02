package silentjson

import "bytes"

func scanJSONStringScalar(src []byte) (int, bool) {
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

func parseShortStringScalar(src []byte) ([]byte, int64) {
	w, c := parseShortStringScalar2(src)
	if c < 0 {
		return nil, -1
	}
	return src[:w], c
}

func parseShortStringScalar2(src []byte) (int64, int64) {
	end, hasEscape := scanJSONStringScalar(src)
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

func appendIntScalar(buf []byte, val int64) []byte {
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

func appendStringScalar(buf []byte, s string) ([]byte, int) {
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

func findQuoteScalar(data []byte) int {
	return bytes.IndexByte(data, '"')
}

func findQuoteOrEscapeScalar(b []byte) (int, bool) {
	idx := bytes.IndexAny(b, "\"\\")
	if idx == -1 {
		return -1, false
	}
	return idx, b[idx] == '\\'
}

func skipSpaceScalar(data []byte, start int) int {
	for i := start; i < len(data); i++ {
		if (charTable[data[i]] & charSpace) == 0 {
			return i
		}
	}
	return len(data)
}

func findObjectBoundariesEarlyExitScalar(data []byte, chunks []Chunk) (int, int) {
	depth := 0
	count := 0
	start := skipSpaceScalar(data, 0)

	for i := start; i < len(data); i++ {
		c := data[i]

		if c == '"' {
			// Fast string scanning: skip until unescaped quote
			i++
			// Most strings don't have escapes - fast path
			for i < len(data) && data[i] != '"' && data[i] != '\\' {
				i++
			}
			// Now handle escapes if needed
			for i < len(data) {
				if data[i] == '\\' && i+1 < len(data) {
					i += 2 // Skip escape sequence
				} else if data[i] == '"' {
					break
				} else {
					i++
				}
			}
			continue
		}

		switch c {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth < 0 {
				if c == '}' {
					if count < len(chunks) {
						chunks[count] = Chunk{Start: start, End: i}
					}
					count++
					return count, 0
				}
				return count, depth
			}
		case ',':
			if depth == 0 {
				if count < len(chunks) {
					chunks[count] = Chunk{Start: start, End: i}
				}
				count++
				// Inline space skipping to avoid function call in hot loop
				i++
				for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
					i++
				}
				i-- // Compensate for loop increment
				start = i
			}
		}
	}
	return count, depth
}

func findObjectBoundariesScalar(data []byte, chunks []Chunk) (int, int) {
	return findObjectBoundariesEarlyExitScalar(data, chunks)
}

func findArrayElementsEarlyExitScalar(data []byte, chunks []Chunk) (int, int) {
	depth := 0
	count := 0
	start := skipSpaceScalar(data, 0)

	for i := start; i < len(data); i++ {
		c := data[i]

		if c == '"' {
			// Fast string scanning: skip until unescaped quote
			i++
			// Most strings don't have escapes - fast path
			for i < len(data) && data[i] != '"' && data[i] != '\\' {
				i++
			}
			// Now handle escapes if needed
			for i < len(data) {
				if data[i] == '\\' && i+1 < len(data) {
					i += 2 // Skip escape sequence
				} else if data[i] == '"' {
					break
				} else {
					i++
				}
			}
			continue
		}

		switch c {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth < 0 {
				if c == ']' {
					if count < len(chunks) {
						chunks[count] = Chunk{Start: start, End: i}
					}
					count++
					return count, 0
				}
				return count, depth
			}
		case ',':
			if depth == 0 {
				if count < len(chunks) {
					chunks[count] = Chunk{Start: start, End: i}
				}
				count++
				// Inline space skipping to avoid function call in hot loop
				i++
				for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
					i++
				}
				i-- // Compensate for loop increment
				start = i
			}
		}
	}
	return count, depth
}

func skipValueScalar(raw []byte, start int) int {
	return skipValue(raw, start)
}
