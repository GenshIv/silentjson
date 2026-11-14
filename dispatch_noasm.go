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
