//go:build amd64

package silentjson

// archArraySplitter exposes the raw AVX2 splitter for the boundary sweep
// tests. It is enabled and shipped, so it is always tested directly.
func archArraySplitter() (func([]byte, []Chunk) (int, int), bool) {
	if hasAVX2 {
		return findArrayElementsEarlyExitAVX2, true
	}
	return nil, false
}
