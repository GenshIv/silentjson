//go:build !amd64

package silentjson

import "unsafe"

// This file keeps the package building on architectures without an AVX2
// dispatch table. The string fast path in primitives.go calls
// findBlockMasksASM and parseStringBlockASM unconditionally; on amd64 those
// live in dispatch_amd64.go and pick an AVX2 implementation at runtime,
// everywhere else they are thin wrappers over the scalar reference.

// hasAVX2 is false off amd64; it exists so arch-independent code and tests
// can query the dispatch decision without a build tag.
var hasAVX2 = false

// findBlockMasksASM computes quote and backslash masks for a 32-byte block.
// Caller must ensure src has at least 32 readable bytes.
func findBlockMasksASM(src *byte) (quoteMask uint32, slashMask uint32) {
	return findBlockMasksScalar(unsafe.Slice(src, 32))
}

// parseStringBlockASM decodes one 32-byte block of JSON string content.
// Caller must ensure src has at least 32 readable bytes and dst at least
// 64 writable bytes.
func parseStringBlockASM(src *byte, dst *byte) (consumed int, written int, status int) {
	return parseStringBlockScalar(src, dst)
}

// parseJSONStringCompleteASM finds the closing quote, decodes all escapes,
// and writes the complete decoded string to dst in one call. Returns (written, consumed).
func parseJSONStringCompleteASM(src []byte, dst []byte) (int64, int64) {
	// Single-pass scalar implementation: find closing quote and decode escapes simultaneously
	written := 0
	for i := 0; i < len(src); i++ {
		if src[i] == '"' {
			return int64(written), int64(i + 1)
		}
		if src[i] == '\\' && i+1 < len(src) {
			i++
			switch src[i] {
			case 'n':
				dst[written] = '\n'
				written++
			case '"':
				dst[written] = '"'
				written++
			case '\\':
				dst[written] = '\\'
				written++
			case 'r':
				dst[written] = '\r'
				written++
			case 't':
				dst[written] = '\t'
				written++
			case '/':
				dst[written] = '/'
				written++
			case 'b':
				dst[written] = '\b'
				written++
			case 'f':
				dst[written] = '\f'
				written++
			case 'u':
				// Unicode escape: \uXXXX
				if i+6 > len(src) {
					return -1, -1 // Truncated unicode escape
				}
				codepoint, ok := parseHex4(src[i+1 : i+5])
				if !ok {
					return -3, -1 // Invalid hex digit
				}
				if codepoint >= 0xD800 && codepoint <= 0xDBFF && i+12 <= len(src) {
					// High surrogate - check for low surrogate
					if src[i+5] == '\\' && src[i+6] == 'u' {
						lowCodepoint, ok := parseHex4(src[i+7 : i+11])
						if !ok {
							return -3, -1 // Invalid hex digit in low surrogate
						}
						if lowCodepoint >= 0xDC00 && lowCodepoint <= 0xDFFF {
							// Valid surrogate pair - combine
							codepoint = 0x10000 + ((codepoint-0xD800)<<10) + (lowCodepoint - 0xDC00)
							i += 10 // Skip past the low surrogate escape
							writeUTF8(dst, &written, codepoint)
						} else {
							return -2, -1 // Invalid low surrogate range
						}
					} else {
						return -2, -1 // Unpaired high surrogate
					}
				} else if codepoint >= 0xDC00 && codepoint <= 0xDFFF {
					return -2, -1 // Lone low surrogate
				} else {
					// Regular unicode character
					writeUTF8(dst, &written, codepoint)
				}
			default:
				return -2, -1 // Unknown escape sequence
			}
		} else {
			dst[written] = src[i]
			written++
		}
	}
	return -1, -1 // No closing quote found
}

// writeUTF8 encodes a codepoint as UTF-8 and writes it to dst, updating written.
func writeUTF8(dst []byte, written *int, codepoint rune) {
	if codepoint <= 0x7F {
		dst[*written] = byte(codepoint)
		*written++
	} else if codepoint <= 0x7FF {
		dst[*written] = byte(0xC0 | (codepoint >> 6))
		dst[*written+1] = byte(0x80 | (codepoint & 0x3F))
		*written += 2
	} else if codepoint <= 0xFFFF {
		dst[*written] = byte(0xE0 | (codepoint >> 12))
		dst[*written+1] = byte(0x80 | ((codepoint >> 6) & 0x3F))
		dst[*written+2] = byte(0x80 | (codepoint & 0x3F))
		*written += 3
	} else {
		dst[*written] = byte(0xF0 | (codepoint >> 18))
		dst[*written+1] = byte(0x80 | ((codepoint >> 12) & 0x3F))
		dst[*written+2] = byte(0x80 | ((codepoint >> 6) & 0x3F))
		dst[*written+3] = byte(0x80 | (codepoint & 0x3F))
		*written += 4
	}
}
