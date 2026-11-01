package silentjson

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
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
	// Pointer types
	TypeIntPtr
	TypeStringPtr
	TypeBoolPtr
	TypeFloatPtr
	TypeStructPtr
	// Raw JSON message
	TypeRawMessage
	// Fixed-width unsigned ints (8/16/32); uint/uint64 are rejected at build time
	TypeUint
)

// ---------- Strict JSON number parsing (P1-1 fix) ----------

// parseJSONInt parses a JSON integer token: -?(0|[1-9][0-9]*).
// It rejects fractions, exponents, leading zeros, garbage and overflow,
// matching encoding/json strictness for integer fields.
func parseJSONInt(b []byte) (int64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	i := 0
	neg := b[0] == '-'
	if neg {
		i++
	}
	if i >= len(b) {
		return 0, false
	}
	if b[i] == '0' {
		if len(b)-i != 1 {
			return 0, false // leading zero: "00", "01"
		}
		return 0, true
	}
	var u uint64
	for ; i < len(b); i++ {
		c := b[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		d := uint64(c - '0')
		if u > (math.MaxUint64-d)/10 {
			return 0, false // uint64 overflow
		}
		u = u*10 + d
	}
	if neg {
		if u > 1<<63 {
			return 0, false
		}
		if u == 1<<63 {
			return math.MinInt64, true
		}
		return -int64(u), true
	}
	if u > math.MaxInt64 {
		return 0, false
	}
	return int64(u), true
}

// validJSONNumber reports whether b is a valid JSON number token:
// -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?
func validJSONNumber(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	i := 0
	if b[0] == '-' {
		i++
	}
	if i >= len(b) {
		return false
	}
	if b[i] == '0' {
		i++
		if i < len(b) && b[i] >= '0' && b[i] <= '9' {
			return false
		}
	} else if b[i] >= '1' && b[i] <= '9' {
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
		}
	} else {
		return false
	}
	if i < len(b) && b[i] == '.' {
		i++
		if i >= len(b) || b[i] < '0' || b[i] > '9' {
			return false
		}
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
		}
	}
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		i++
		if i < len(b) && (b[i] == '+' || b[i] == '-') {
			i++
		}
		if i >= len(b) || b[i] < '0' || b[i] > '9' {
			return false
		}
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
		}
	}
	return i == len(b)
}

// parseJSONFloat parses a strict JSON number token into a float64.
// Range errors (±Inf) are rejected, matching encoding/json.
// pow10[i] = 10^i for i in [0,22]: every value is exactly representable
// as a float64, which is what makes the fast path below bit-exact.
var pow10 = [23]float64{1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22}

// parseFloatFast handles the common number shapes (up to 16 significant
// digits, |decimal exponent| <= 22) in a single validating pass.
//
// Bit-exactness argument: while the accumulated mantissa stays <= 2^53
// float64(mant) is exact, and 10^k is exactly representable for k <= 22,
// so mant*10^k is the exact integer value of the literal (correctly rounded
// by one IEEE multiply) and mant/10^k is the correctly rounded true
// quotient — both identical to what strconv.ParseFloat returns. The moment
// the mantissa passes 2^53 (e.g. the 16th digit of 9007199254740995)
// float64(mant) becomes inexact and one more rounding step would
// double-round up to 1 ULP away from strconv — so the fast path refuses
// and falls back. Anything else outside the envelope (more digits, bigger
// exponents, subnormals near the boundaries) also reports not-ok and falls
// back to the strconv path, which also produces the correct strict grammar
// error for garbage.
func parseFloatFast(b []byte) (float64, bool) {
	i := 0
	neg := false
	if i < len(b) && b[i] == '-' {
		neg = true
		i++
	}
	if i >= len(b) {
		return 0, false
	}

	var mant uint64
	digits := 0

	// integer part ("0" alone, or [1-9][0-9]*)
	if b[i] == '0' {
		mant = 0
		digits = 1
		i++
	} else if b[i] >= '1' && b[i] <= '9' {
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			d := uint64(b[i] - '0')
			mant = mant*10 + d
			if mant > 1<<53 {
				// float64(mant) would be inexact: the single
				// multiply/divide then double-rounds and can
				// land 1 ULP off strconv (e.g. 9007199254740995e-1).
				// mant == 2^53 stays fast: it is exact.
				return 0, false
			}
			digits++
			i++
		}
	} else {
		return 0, false
	}

	exp10 := 0
	// fraction
	if i < len(b) && b[i] == '.' {
		i++
		if i >= len(b) || b[i] < '0' || b[i] > '9' {
			return 0, false
		}
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			d := uint64(b[i] - '0')
			mant = mant*10 + d
			if mant > 1<<53 {
				return 0, false // same 2^53 boundary as above
			}
			digits++
			exp10--
			i++
		}
	}
	if digits == 0 {
		return 0, false
	}
	// exponent
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		i++
		esign := 1
		if i < len(b) && (b[i] == '+' || b[i] == '-') {
			if b[i] == '-' {
				esign = -1
			}
			i++
		}
		if i >= len(b) || b[i] < '0' || b[i] > '9' {
			return 0, false
		}
		ev := 0
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			ev = ev*10 + int(b[i]-'0')
			if ev > 100000 {
				return 0, false // absurd exponent: fallback
			}
			i++
		}
		exp10 += esign * ev
	}
	if i != len(b) {
		return 0, false // trailing garbage: let the strict path report it
	}
	if exp10 > 22 || exp10 < -22 {
		return 0, false
	}

	var v float64
	if exp10 >= 0 {
		v = float64(mant) * pow10[exp10]
	} else {
		v = float64(mant) / pow10[-exp10]
	}
	if neg {
		v = -v
	}
	// overflow/underflow to Inf/0 means we left the exactly-representable
	// envelope (mant < 2^53 and |exp| <= 22 cannot overflow, but be safe
	// against future edits) — fall back rather than guess.
	// mant < 2^53 and |exp10| <= 22 keep the result between ~4.5e-23 and
	// ~9e37 — far from the Inf/subnormal boundaries — but guard anyway so
	// a future edit to the envelope cannot silently corrupt values.
	if (v == 0 && mant != 0) || v != v || v > 1.7976931348623157e308 || v < -1.7976931348623157e308 {
		return 0, false
	}
	return v, true
}

func parseJSONFloat(b []byte) (float64, bool) {
	if v, ok := parseFloatFast(b); ok {
		return v, true
	}
	if !validJSONNumber(b) {
		return 0, false
	}
	v, err := strconv.ParseFloat(unsafe.String(unsafe.SliceData(b), len(b)), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// ---------- Fixed-width integer helpers (P0-1/P0-2 fix) ----------

func kindIsUnsigned(k reflect.Kind) bool {
	switch k {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return true
	}
	return false
}

func kindBits(k reflect.Kind) uint8 {
	switch k {
	case reflect.Int8, reflect.Uint8:
		return 8
	case reflect.Int16, reflect.Uint16:
		return 16
	case reflect.Int32, reflect.Uint32:
		return 32
	case reflect.Float32:
		return 32
	}
	return 64
}

func intFitsWidth(v int64, bits uint8, unsigned bool) bool {
	switch bits {
	case 8:
		if unsigned {
			return v >= 0 && v <= math.MaxUint8
		}
		return v >= math.MinInt8 && v <= math.MaxInt8
	case 16:
		if unsigned {
			return v >= 0 && v <= math.MaxUint16
		}
		return v >= math.MinInt16 && v <= math.MaxInt16
	case 32:
		if unsigned {
			return v >= 0 && v <= math.MaxUint32
		}
		return v >= math.MinInt32 && v <= math.MaxInt32
	}
	if unsigned {
		return v >= 0
	}
	return true
}

func loadIntWidth(ptr unsafe.Pointer, bits uint8, unsigned bool) int64 {
	switch bits {
	case 8:
		if unsigned {
			return int64(*(*uint8)(ptr))
		}
		return int64(*(*int8)(ptr))
	case 16:
		if unsigned {
			return int64(*(*uint16)(ptr))
		}
		return int64(*(*int16)(ptr))
	case 32:
		if unsigned {
			return int64(*(*uint32)(ptr))
		}
		return int64(*(*int32)(ptr))
	}
	return *(*int64)(ptr)
}

func storeIntWidth(ptr unsafe.Pointer, v int64, bits uint8, unsigned bool) {
	switch bits {
	case 8:
		if unsigned {
			*(*uint8)(ptr) = uint8(v)
		} else {
			*(*int8)(ptr) = int8(v)
		}
	case 16:
		if unsigned {
			*(*uint16)(ptr) = uint16(v)
		} else {
			*(*int16)(ptr) = int16(v)
		}
	case 32:
		if unsigned {
			*(*uint32)(ptr) = uint32(v)
		} else {
			*(*int32)(ptr) = int32(v)
		}
	default:
		*(*int64)(ptr) = v
	}
}

// ---------- Non-mutating string decoding (P1-2/P1-3 fix) ----------

func parseHex4(b []byte) (rune, bool) {
	var v rune
	for _, c := range b {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			v |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= rune(c-'A') + 10
		default:
			return 0, false
		}
	}
	return v, true
}

// decodeEscapedString decodes a JSON string body (src starts right after the
// opening quote) into a freshly allocated buffer. The input is NEVER modified.
// Handles \uXXXX escapes including surrogate pairs; invalid escapes are errors.
func decodeEscapedString(src []byte) ([]byte, int64, error) {
	buf := make([]byte, 0, len(src))
	i := 0
	for {
		if i >= len(src) {
			return nil, 0, ErrUnexpectedEOF
		}
		c := src[i]
		if c == '"' {
			return buf, int64(i + 1), nil
		}
		if c == '\\' {
			if i+1 >= len(src) {
				return nil, 0, ErrUnexpectedEOF
			}
			e := src[i+1]
			i += 2
			switch e {
			case '"':
				buf = append(buf, '"')
			case '\\':
				buf = append(buf, '\\')
			case '/':
				buf = append(buf, '/')
			case 'n':
				buf = append(buf, '\n')
			case 't':
				buf = append(buf, '\t')
			case 'r':
				buf = append(buf, '\r')
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case 'u':
				if i+4 > len(src) {
					return nil, 0, fmt.Errorf("%w: truncated \\u escape", ErrTypeMismatch)
				}
				cp, ok := parseHex4(src[i : i+4])
				if !ok {
					return nil, 0, fmt.Errorf("%w: invalid \\u escape", ErrTypeMismatch)
				}
				i += 4
				r := cp
				if utf16.IsSurrogate(r) {
					if i+6 <= len(src) && src[i] == '\\' && src[i+1] == 'u' {
						if cp2, ok2 := parseHex4(src[i+2 : i+6]); ok2 {
							if dec := utf16.DecodeRune(r, cp2); dec != unicode.ReplacementChar {
								buf = utf8.AppendRune(buf, dec)
								i += 6
								continue
							}
						}
					}
					r = utf8.RuneError // unpaired surrogate -> U+FFFD, like encoding/json
				}
				buf = utf8.AppendRune(buf, r)
			default:
				return nil, 0, fmt.Errorf("%w: invalid escape sequence \\%c", ErrTypeMismatch, e)
			}
			continue
		}
		buf = append(buf, c)
		i++
	}
}

// readJSONString reads a JSON string whose OPENING QUOTE HAS ALREADY BEEN
// CONSUMED (src starts right after it). Returns:
//   - s: the decoded string value (zero-copy alias of the input for strings
//     without escapes, unless copyStrings is true; a fresh allocation for
//     escaped strings — the input buffer is never modified)
//   - content: the decoded bytes s points to (useful for key matching)
//   - consumed: number of input bytes read, including the closing quote
func readJSONString(src []byte, copyStrings bool) (s string, content []byte, consumed int64, err error) {
	idx, isEsc := findQuoteOrEscapeASM(src)
	if idx < 0 {
		return "", nil, 0, ErrUnexpectedEOF
	}
	if !isEsc {
		content = src[:idx]
		if copyStrings {
			return string(content), content, int64(idx + 1), nil
		}
		return unsafe.String(unsafe.SliceData(content), idx), content, int64(idx + 1), nil
	}
	buf, consumed, err := decodeEscapedString(src)
	if err != nil {
		return "", nil, 0, err
	}
	return unsafe.String(unsafe.SliceData(buf), len(buf)), buf, consumed, nil
}

func parseJSONString(raw []byte, start int, copyStrings bool) (string, int, error) {
	s, _, consumed, err := readJSONString(raw[start:], copyStrings)
	return s, int(consumed), err
}
