//go:build amd64

package silentjson

import (
	"golang.org/x/sys/cpu"
	"unsafe"
)

// CPU feature flags - exposed as variables for testability. Tests can override
// these to verify dispatch logic selects the correct implementation for different
// CPU feature combinations.
var hasAVX2 = cpu.X86.HasAVX2

// VPCLMULQDQ (VEX-encoded, XMM operands) requires CPUID leaf 1 ECX bit 1
// (PCLMULQDQ feature) plus AVX support for the VEX prefix. There is no
// separate CPUID bit for VPCLMULQDQ vs PCLMULQDQ - they share the same
// feature flag. The AVX512 variant (EVEX-encoded, ZMM operands) uses a
// different bit (leaf 7 EBX bit 30), but that's not used here.
var hasPCLMULQDQ = cpu.X86.HasPCLMULQDQ

func parseShortStringASM2(src []byte) (int64, int64) {
	if hasAVX2 {
		return parseShortStringAVX2_2(src)
	}
	return parseShortStringScalar2(src)
}

// parseJSONStringCompleteASM finds the closing quote, decodes all escapes,
// and writes the complete decoded string to dst in one call. Returns (written, consumed).
func parseJSONStringCompleteASM(src []byte, dst []byte) (int64, int64) {
	if hasAVX2 {
		return parseJSONStringCompleteAVX2(src, dst)
	}
	// Scalar fallback: find quote, then decode with full escape handling
	end, _ := scanJSONStringScalar(src)
	if end < 0 {
		return -1, -1
	}
	written := 0
	for i := 0; i < end; i++ {
		if src[i] == '\\' && i+1 < end {
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
				if i+6 > end {
					return -1, -1 // Truncated unicode escape
				}
				codepoint, ok := parseHex4(src[i+1 : i+5])
				if !ok {
					return -3, -1 // Invalid hex digit
				}
				if codepoint >= 0xD800 && codepoint <= 0xDBFF && i+12 <= end {
					// High surrogate - check for low surrogate
					if src[i+7] == '\\' && src[i+8] == 'u' {
						lowCodepoint, ok := parseHex4(src[i+9 : i+13])
						if ok && lowCodepoint >= 0xDC00 && lowCodepoint <= 0xDFFF {
							// Valid surrogate pair - combine
							codepoint = 0x10000 + ((codepoint-0xD800)<<10) + (lowCodepoint - 0xDC00)
							i += 6 // Skip the low surrogate escape
							writeUTF8(dst, &written, codepoint)
						} else {
							return -2, -1 // Invalid surrogate pair
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
	return int64(written), int64(end + 1)
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

// The *EarlyExit splitters have assembly (AVX2) implementations with a
// corrected escape-carry: a backslash ending a 32-byte SIMD block sets a
// carry that now correctly skips the escaped byte at the start of the
// next block (and of the scalar tail) in ALL masks. The original version
// missed the backslash mask and the scalar tail entirely, which flipped
// the in-string parity and silently merged/dropped whole runs of records
// (see TestSplitterEscapeCarry* and TestSplitterMarshalRoundTrip).
func findObjectBoundariesEarlyExitASM(data []byte, chunks []Chunk) (int, int) {
	if hasAVX2 {
		return findObjectBoundariesEarlyExitAVX2(data, chunks)
	}
	return findObjectBoundariesEarlyExitScalar(data, chunks)
}

// useArraySplitterAVX2 is a pure function that determines whether the AVX2
// array splitter implementation should be used based on CPU features.
// This is testable without executing any assembly instructions.
func useArraySplitterAVX2(avx2, pclmul bool) bool {
	return avx2 && pclmul
}

func findArrayElementsEarlyExitASM(data []byte, chunks []Chunk) (int, int) {
	if useArraySplitterAVX2(hasAVX2, hasPCLMULQDQ) {
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

// findQuoteOrEscapeASM finds the first '"' or '\\' in b. The AVX2
// implementation (32-byte blocks, scalar tail) replaced the two
// bytes.IndexByte scans in findQuoteOrEscapeScalar on the hot string path;
// equivalence is pinned by TestFindQuoteOrEscapeSweep.
func findQuoteOrEscapeASM(b []byte) (idx int, isEscape bool) {
	if hasAVX2 {
		return findQuoteOrEscapeAVX2(b)
	}
	return findQuoteOrEscapeScalar(b)
}

// findBlockMasksASM computes quote and backslash masks for a block.
// Caller must ensure src has at least 32 bytes available.
func findBlockMasksASM(src *byte) (quoteMask uint32, slashMask uint32) {
	if hasAVX2 {
		return findBlockMasksAVX2(src)
	}
	var block [32]byte
	copy(block[:], unsafe.Slice(src, 32))
	return findBlockMasksScalar(block[:])
}

// parseStringBlockASM decodes one 32-byte block of JSON string content.
// Caller must ensure src has at least 32 readable bytes and dst at least
// 64 writable bytes.
func parseStringBlockASM(src *byte, dst *byte) (consumed int, written int, status int) {
	if hasAVX2 {
		return parseStringBlockAVX2(src, dst)
	}
	return parseStringBlockScalar(src, dst)
}
