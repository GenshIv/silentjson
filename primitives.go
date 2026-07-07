package silentjson

import (
	"strconv"
	"unsafe"
)

type FieldType int

type Chunk struct {
	Start, End int
}

// Precomputed Look-Up Table (LUT) for O(1) character classification
var charTable = [256]uint16{
	' ':  charSpace,
	'\n': charSpace,
	'\t': charSpace,
	'\r': charSpace,

	'"':  charString,
	'\\': charEscape,

	'{': charOpenBrace,
	'}': charCloseBrace,
	'[': charOpenBracket,
	']': charCloseBracket,
	':': charColon,
	',': charComma,

	'0': charDigit,
	'1': charDigit,
	'2': charDigit,
	'3': charDigit,
	'4': charDigit,
	'5': charDigit,
	'6': charDigit,
	'7': charDigit,
	'8': charDigit,
	'9': charDigit,
	'.': charDot,
	'e': charLetterE,
}

const (
	charNone         uint16 = 0
	charSpace        uint16 = 1 << 0 // Spaces, tabs, newlines
	charString       uint16 = 1 << 1 // Quote "
	charEscape       uint16 = 1 << 2 // Backslash \
	charOpenBrace    uint16 = 1 << 3
	charCloseBrace   uint16 = 1 << 4
	charOpenBracket  uint16 = 1 << 5
	charCloseBracket uint16 = 1 << 6
	charColon        uint16 = 1 << 7
	charComma        uint16 = 1 << 8
	charDigit        uint16 = 1 << 9
	charDot          uint16 = 1 << 10
	charLetterE      uint16 = 1 << 11

	charStruct   = charOpenBrace | charCloseBrace | charOpenBracket | charCloseBracket | charColon | charComma
	maskValueEnd = charStruct | charSpace
)

const (
	nullMagic        = uint32(0x6C6C756E) // "null"
	trueMagic        = uint32(0x65757274) // "true"
	falsePrefixMagic = uint32(0x736C6166) // "fals"

	TypeInt FieldType = iota
	TypeString
	TypeBool
	TypeFloat
	TypeStruct
	TypeStringSlice
	TypeIntSlice
	TypeStructSlice
)

func fastParseInt(buf []byte) int {
	res := 0
	for _, b := range buf {
		if (charTable[b] & charDigit) != 0 {
			res = res*10 + int(b-'0')
		}
	}
	return res
}

func fastParseFloat(buf []byte) float64 {
	val, _ := strconv.ParseFloat(unsafe.String(unsafe.SliceData(buf), len(buf)), 64)
	return val
}

func parseJSONString(raw []byte, start int, copyStrings bool) (string, int, error) {
	written, consumed := parseShortStringASM2(raw[start:])
	if consumed < 0 {
		return "", 0, ErrUnexpectedEOF
	}
	decoded := raw[start : start+int(written)]
	if copyStrings {
		return string(decoded), int(consumed), nil
	}
	return unsafe.String(unsafe.SliceData(decoded), len(decoded)), int(consumed), nil
}

// unescapeStringInPlace modifies the provided raw slice in-place, removing escape characters.
// It returns a zero-copy string pointing to the same memory segment.
// WARNING: This mutates the underlying rawJSON byte array.
func unescapeStringInPlace(raw []byte) string {
	writeIdx := 0
	for readIdx := 0; readIdx < len(raw); readIdx++ {
		if (charTable[raw[readIdx]]&charEscape) != 0 && readIdx+1 < len(raw) {
			readIdx++ // Skip the backslash
			switch raw[readIdx] {
			case 'n':
				raw[writeIdx] = '\n'
			case '"':
				raw[writeIdx] = '"'
			case '\\':
				raw[writeIdx] = '\\'
			case 'r':
				raw[writeIdx] = '\r'
			case 't':
				raw[writeIdx] = '\t'
			case '/':
				raw[writeIdx] = '/'
			case 'b':
				raw[writeIdx] = '\b'
			case 'f':
				raw[writeIdx] = '\f'
			default:
				raw[writeIdx] = raw[readIdx] // Fallback for unknown escapes
			}
		} else {
			raw[writeIdx] = raw[readIdx]
		}
		writeIdx++
	}
	// Zero-copy return of the modified underlying buffer
	return unsafe.String(unsafe.SliceData(raw), writeIdx)
}
