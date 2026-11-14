package silentjson

import (
	"bytes"
	"unsafe"
)

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

// findQuoteOrEscapeScalar finds the first '"' or '\\'. Two IndexByte scans
// are used instead of bytes.IndexAny: IndexByte is SIMD-accelerated by the
// runtime with no setup cost, while IndexAny builds an ASCII set on every
// call, which showed up as a top CPU consumer in parse profiles after the
// assembly short-string scanner was replaced by readJSONString.
func findQuoteOrEscapeScalar(b []byte) (int, bool) {
	q := bytes.IndexByte(b, '"')
	e := bytes.IndexByte(b, '\\')
	switch {
	case q == -1 && e == -1:
		return -1, false
	case q == -1:
		return e, true
	case e == -1:
		return q, false
	case e < q:
		return e, true
	default:
		return q, false
	}
}

// findBlockMasksScalar computes quote and backslash masks for a block.
// blockLen must be <= 32. Returns (quoteMask, slashMask).
func findBlockMasksScalar(src []byte) (quoteMask uint32, slashMask uint32) {
	for i := 0; i < len(src) && i < 32; i++ {
		if src[i] == '"' {
			quoteMask |= 1 << i
		}
		if src[i] == '\\' {
			slashMask |= 1 << i
		}
	}
	return quoteMask, slashMask
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
				start = i + 1
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
						count++
					}
					return count, 0
				}
				return count, depth
			}
		case ',':
			if depth == 0 {
				if count >= len(chunks) {
					// Early exit, mirroring the AVX2 contract: stop as soon
					// as the chunk buffer is full. Callers resume scanning
					// from chunks[count-1].End (the position of this comma);
					// the empty Start==End chunk produced on resume is
					// filtered out by the callers.
					return count, depth
				}
				chunks[count] = Chunk{Start: start, End: i}
				count++
				// Inline space skipping to avoid function call in hot loop
				i++
				for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
					i++
				}
				i-- // Compensate for loop increment
				start = i + 1
			}
		}
	}
	return count, depth
}

func skipValueScalar(raw []byte, start int) int {
	return skipValue(raw, start)
}

// ---------- JSON string block parsing ----------

// Status values returned by parseStringBlockScalar / parseStringBlockAVX2.
const (
	// blockEOB: the 32-byte block was consumed without hitting a terminator.
	// consumed == 32 and the caller continues with the next block.
	blockEOB = 0
	// blockQuote: the closing quote was found. consumed includes the quote
	// byte, written does not.
	blockQuote = 1
	// blockDefer: src[consumed] holds a '\' that the block procedure refuses
	// to decode - a \uXXXX escape, an unknown escape, or a backslash sitting
	// on the last byte of the block (its partner lives in the next block).
	// The caller decodes it in Go and resumes.
	blockDefer = 2
)

// escTable maps the byte following a backslash to its decoded value for the
// simple (single-byte) JSON escapes. Zero means "not a simple escape": either
// \u, which expands to UTF-8 and may consume a surrogate pair, or an invalid
// sequence. Zero is an unambiguous marker because no valid simple escape
// decodes to 0x00. parse_string_block_amd64.s carries a byte-identical copy.
var escTable = [256]byte{
	'"':  '"',
	'\\': '\\',
	'/':  '/',
	'b':  '\b',
	'f':  '\f',
	'n':  '\n',
	'r':  '\r',
	't':  '\t',
}

// parseStringBlockScalar is the reference implementation of the block
// procedure described on parseStringBlockAVX2: it copies plain bytes and
// decodes simple escapes from one 32-byte block of JSON string content,
// stopping at the closing quote or at an escape only Go can handle.
//
// src must address at least 32 readable bytes and dst at least 64 writable
// bytes - the AVX2 version writes whole 32-byte vectors from a cursor that
// can sit at offset 31, so bytes past `written` are left undefined by both
// implementations and only dst[:written] is meaningful.
func parseStringBlockScalar(src *byte, dst *byte) (consumed int, written int, status int) {
	s := unsafe.Slice(src, 32)
	d := unsafe.Slice(dst, 64)
	o := 0
	for i := 0; i < 32; i++ {
		c := s[i]
		if c == '"' {
			return i + 1, o, blockQuote
		}
		if c == '\\' {
			if i+1 == 32 {
				return i, o, blockDefer
			}
			e := escTable[s[i+1]]
			if e == 0 {
				return i, o, blockDefer
			}
			d[o] = e
			o++
			i++ // consume the escaped byte; the loop consumes the backslash
			continue
		}
		d[o] = c
		o++
	}
	return 32, o, blockEOB
}
