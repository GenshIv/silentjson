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
