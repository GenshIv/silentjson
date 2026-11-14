//go:build amd64

package silentjson

//go:noescape
func parseShortStringAVX2_2(src []byte) (int64, int64)

//go:noescape
func findQuoteAVX2(data []byte) (index int)

//go:noescape
func appendIntAVX2(buf []byte, val int64) []byte

// appendStringAVX2 copies string s to buf, adding quotes.
// In one pass, it copies bytes and scans for special characters.
// Returns the new buf and the position of the first special character (-1 if none).
//
//go:noescape
func appendStringAVX2(buf []byte, s string) ([]byte, int)

//go:noescape
func findObjectBoundariesAVX2(data []byte, chunks []Chunk) (ret0 int, ret1 int)

//go:noescape
func findObjectBoundariesEarlyExitAVX2(data []byte, chunks []Chunk) (int, int)

//go:noescape
func findArrayElementsEarlyExitAVX2(data []byte, chunks []Chunk) (int, int)

//go:noescape
func skipValueAVX2(raw []byte, start int) int

//go:noescape
func skipSpaceAVX2(data []byte, start int) int

// findQuoteOrEscapeAVX2 finds the first '"' or '\' in b, scanning 32-byte
// blocks with AVX2 (scalar loop for the sub-32-byte tail and short inputs).
// Returns the index of the first hit (-1 if none) and whether it is a
// backslash. Implemented in find_quote_amd64.s; correctness is pinned by
// TestFindQuoteOrEscapeSweep against the scalar reference.
//
//go:noescape
func findQuoteOrEscapeAVX2(b []byte) (idx int, isEscape bool)

// findBlockMasksAVX2 computes quote and backslash masks for a 32-byte block.
// Returns (quoteMask, slashMask) as two uint32 values where bit i is set if
// byte i matches the respective character. Caller must guarantee src points
// to at least 32 bytes. Implemented in find_block_masks_amd64.s.
//
//go:noescape
func findBlockMasksAVX2(src *byte) (quoteMask uint32, slashMask uint32)

// parseStringBlockAVX2 decodes one 32-byte block of JSON string content
// (src points just past the opening quote or at any later position inside
// the string body), copying plain bytes and folding simple escapes into dst.
//
// src must address at least 32 readable bytes and dst at least 64 writable
// bytes: the routine emits whole 32-byte vector stores from a write cursor
// that can sit at offset 31, advancing the cursor only by the length of the
// real run. Bytes in dst past the returned written count are undefined.
//
// Returns the number of input bytes consumed, the number of output bytes
// produced, and one of blockEOB / blockQuote / blockDefer. parseStringBlockScalar
// in scalar.go is the reference implementation; TestParseStringBlockSweep pins
// the two together.
//
//go:noescape
func parseStringBlockAVX2(src *byte, dst *byte) (consumed int, written int, status int)

// parseJSONStringCompleteAVX2 takes a JSON string starting just after the
// opening quote, finds the closing quote using AVX2 scanning, decodes all
// escape sequences, and writes the complete decoded string to dst. Returns
// the number of bytes written to dst and the number of input bytes consumed
// (including the closing quote).
//
//go:noescape
func parseJSONStringCompleteAVX2(src []byte, dst []byte) (written int64, consumed int64)
