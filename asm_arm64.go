//go:build arm64

package silentjson

// В ARM64-ассемблере реализованы базовые функции поиска:
//go:noescape
func findQuoteAsm(data []byte) (index int)

//go:noescape
func scanJSONStringASM(src []byte) (end int, hasEscape bool)

// Остальные функции пока реализованы как фоллбэки на чистом Go, 
// так как перенос AVX2 в NEON требует отдельной кропотливой работы.

func parseShortStringASM(src []byte) ([]byte, int64) {
	end, _ := scanJSONStringASM(src)
	if end < 0 {
		return nil, -1
	}
	return src[:end], int64(end + 1)
}

func parseShortStringASM2(src []byte) (int64, int64) {
	end, _ := scanJSONStringASM(src)
	if end < 0 {
		return 0, -1
	}
	return int64(end), int64(end + 1)
}

func appendIntASM(buf []byte, val int64) []byte {
    // Basic fallback using strconv or simple loop (placeholder logic)
    // For extreme performance, this should be written out.
	var b [20]byte
	var i = 19
	var neg = val < 0
	if neg {
		val = -val
	}
	for val >= 10 {
		q := val / 10
		b[i] = byte('0' + val - q*10)
		i--
		val = q
	}
	b[i] = byte('0' + val)
	if neg {
		i--
		b[i] = '-'
	}
	return append(buf, b[i:]...)
}

//go:noescape
func appendStringASM(buf []byte, s string) ([]byte, int)


//go:noescape
func skipSpaceASM(data []byte, start int) int

func skipValueASM(raw []byte, start int) int {
	return skipValue(raw, start) // Calls the Go implementation from parser.go
}

//go:noescape
func findQuoteOrEscapeASM(b []byte) (idx int, isEscape bool)


//go:noescape
func findObjectBoundariesASM(data []byte, chunks []Chunk) (int, int)

//go:noescape
func findObjectBoundariesEarlyExitASM(data []byte, chunks []Chunk) (int, int)

//go:noescape
func findArrayElementsEarlyExitASM(data []byte, chunks []Chunk) (int, int)
