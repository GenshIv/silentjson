//go:build arm64

package silentjson

import "os"

// archArraySplitter exposes the raw NEON splitter in explicit audit mode
// only. The NEON splitters are disabled in production (see asm_arm64.go)
// pending the same escape-carry audit the AVX2 versions received; run
// SILENTJSON_AUDIT_NEON=1 go test -run TestSplitter on arm64 hardware to
// execute that audit.
func archArraySplitter() (func([]byte, []Chunk) (int, int), bool) {
	if os.Getenv("SILENTJSON_AUDIT_NEON") == "" {
		return nil, false
	}
	return findArrayElementsEarlyExitNEON, true
}
