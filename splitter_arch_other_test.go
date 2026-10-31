//go:build !amd64 && !arm64

package silentjson

func archArraySplitter() (func([]byte, []Chunk) (int, int), bool) {
	return nil, false
}
