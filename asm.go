//go:build amd64

package silentjson

//go:noescape
func parseShortStringAVX2(src []byte) ([]byte, int64)

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

//go:noescape
func findQuoteOrEscapeAVX2(b []byte) (idx int, isEscape bool)
