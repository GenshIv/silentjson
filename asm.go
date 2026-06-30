//go:build amd64

package silentjson

//go:noescape
func parseShortStringASM(src []byte) ([]byte, int64)

//go:noescape
func parseShortStringASM2(src []byte) (int64, int64)

//go:noescape
func findQuoteAsm(data []byte) (index int)

//go:noescape
func appendIntASM(buf []byte, val int64) []byte

// appendStringASM copies string s to buf, adding quotes.
// In one pass, it copies bytes and scans for special characters.
// Returns the new buf and the position of the first special character (-1 if none).
//
//go:noescape
func appendStringASM(buf []byte, s string) ([]byte, int)

//go:noescape
func findObjectBoundariesASM(data []byte, chunks []Chunk) (ret0 int, ret1 int)

//go:noescape
func findObjectBoundariesEarlyExitASM(data []byte, chunks []Chunk) (int, int)

//go:noescape
func findArrayElementsEarlyExitASM(data []byte, chunks []Chunk) (int, int)

//go:noescape
func skipValueASM(raw []byte, start int) int

//go:noescape
func skipSpaceASM(data []byte, start int) int

//go:noescape
func findQuoteOrEscapeASM(b []byte) (idx int, isEscape bool)
