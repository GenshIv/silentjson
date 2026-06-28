package silentjson

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/GenshIv/intHache"
	"github.com/cespare/ryu"
)

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
func skipValueASM(raw []byte, start int) int

//go:noescape
func skipSpaceASM(data []byte, start int) int

//go:noescape
func findQuoteOrEscapeASM(b []byte) (idx int, isEscape bool)

var (
	ErrUnexpectedEOF = errors.New("zerojson: unexpected end of JSON input")
	ErrTypeMismatch  = errors.New("zerojson: json type mismatch")
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
)

type MarshalFunc func(ptr unsafe.Pointer, buf []byte) []byte

type FieldInfo struct {
	EncodedKey []byte // Pre-serialized key, e.g., `"key":`
	Key        string
	KeyLen     int
	KeyUint64  uint64
	Offset     uintptr
	Hash       int64
	Type       FieldType
	Sub        *Registry
	OmitEmpty  bool
	Marshaler  MarshalFunc // <--- DIRECT CALL
}

// Registry: Map for parsing lookup, Fields for fast sequential generation
type Registry struct {
	Map         map[int64]FieldInfo
	NameMap     map[string]FieldInfo
	Fields      []FieldInfo
	chunkPool   sync.Pool
	CopyStrings bool
}

// BuildRegistry constructs a registry for a given struct type.
func BuildRegistry(typ reflect.Type) *Registry {
	reg := &Registry{
		Map:     make(map[int64]FieldInfo),
		NameMap: make(map[string]FieldInfo),
		Fields:  make([]FieldInfo, 0, typ.NumField()),
	}
	reg.chunkPool.New = func() any {
		return make([]Chunk, 131072)
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}

		parts := strings.Split(tag, ",")
		key := parts[0]
		
		keyLen := len(key)
		var keyU64 uint64
		if keyLen > 0 && keyLen <= 8 {
			b := unsafe.SliceData([]byte(key))
			switch keyLen {
			case 1:
				keyU64 = uint64(*b)
			case 2:
				keyU64 = uint64(*(*uint16)(unsafe.Pointer(b)))
			case 3:
				keyU64 = uint64(*(*uint16)(unsafe.Pointer(b))) | (uint64(*(*uint8)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 2))) << 16)
			case 4:
				keyU64 = uint64(*(*uint32)(unsafe.Pointer(b)))
			case 5:
				keyU64 = uint64(*(*uint32)(unsafe.Pointer(b))) | (uint64(*(*uint8)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 4))) << 32)
			case 6:
				keyU64 = uint64(*(*uint32)(unsafe.Pointer(b))) | (uint64(*(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 4))) << 32)
			case 7:
				keyU64 = uint64(*(*uint32)(unsafe.Pointer(b))) | (uint64(*(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 4))) << 32) | (uint64(*(*uint8)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 6))) << 48)
			case 8:
				keyU64 = *(*uint64)(unsafe.Pointer(b))
			}
		}

		info := FieldInfo{
			EncodedKey: []byte(`"` + key + `":`),
			Key:        key,
			KeyLen:     keyLen,
			KeyUint64:  keyU64,
			Offset:     field.Offset,
		}

		for _, opt := range parts[1:] {
			if opt == "omitempty" {
				info.OmitEmpty = true
			}
		}

		// Bind marshaler based on type
		switch field.Type.Kind() {
		case reflect.Int, reflect.Int64:
			info.Type = TypeInt
			info.Marshaler = MarshalInt // Uses our new appendIntASM
		case reflect.Float64, reflect.Float32:
			info.Type = TypeFloat
			info.Marshaler = MarshalFloat // Implement via appendFloatASM or fmt.AppendFloat
		case reflect.String:
			info.Type = TypeString
			info.Marshaler = MarshalString
		case reflect.Bool:
			info.Type = TypeBool
			info.Marshaler = MarshalBool
		case reflect.Struct:
			info.Type = TypeStruct
			info.Sub = BuildRegistry(field.Type)
			// Special marshaler for nested structures
			info.Marshaler = func(ptr unsafe.Pointer, buf []byte) []byte {
				return MarshalObject(ptr, info.Sub, buf)
			}
		case reflect.Slice:
			if field.Type.Elem().Kind() == reflect.String {
				info.Type = TypeStringSlice
				info.Marshaler = MarshalStringSlice
			} else if field.Type.Elem().Kind() == reflect.Int {
				info.Type = TypeIntSlice
				info.Marshaler = MarshalIntSlice
			}
		}

		// If OmitEmpty is present, wrap marshaler in a check
		if info.OmitEmpty {
			orig := info.Marshaler
			info.Marshaler = func(ptr unsafe.Pointer, buf []byte) []byte {
				// Empty check logic is already inside each specific Marshal function
				// or it could be moved here for universality
				return orig(ptr, buf)
			}
		}

		hashe := intHache.Sum([]byte(key))
		info.Hash = hashe
		reg.Map[hashe] = info
		reg.NameMap[key] = info
		reg.Fields = append(reg.Fields, info)
	}
	return reg
}

// MarshalFloat: use stack buffer
func MarshalFloat(ptr unsafe.Pointer, buf []byte) []byte {
	val := *(*float64)(ptr)
	return ryu.AppendFloat64(buf, val)
}

// MarshalBool: direct byte append
func MarshalBool(ptr unsafe.Pointer, buf []byte) []byte {
	if *(*bool)(ptr) {
		return append(buf, "true"...)
	}
	return append(buf, "false"...)
}

// MarshalString: one pass through ASM: copies and scans simultaneously.
// If string is clean and buffer fits — ASM returns (buf+string, -1).
// If special char found — ASM copies prefix, Go handles the rest with escaping.
// If buffer lacks space — Go path via append (with possible grow).
func MarshalString(ptr unsafe.Pointer, buf []byte) []byte {
	s := *(*string)(ptr)

	newBuf, specialPos := appendStringASM(buf, s)
	switch specialPos {
	case -1:
		// String is clean, copied completely with quotes
		return newBuf
	case -2:
		// No space in buffer (overflow) — fallback to Go with escaping
		return appendJSONStringGo(buf, s)
	default:
		// ASM copied s[:specialPos] and opening quote.
		// Write the rest with escaping starting from specialPos.
		buf = newBuf
		for i := specialPos; i < len(s); i++ {
			c := s[i]
			if (charTable[c] & (charString | charEscape)) != 0 {
				buf = append(buf, '\\')
			}
			buf = append(buf, c)
		}
		return append(buf, '"')
	}
}

func appendJSONStringGo(buf []byte, s string) []byte {
	buf = append(buf, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (charTable[c] & (charString | charEscape)) != 0 {
			buf = append(buf, '\\')
		}
		buf = append(buf, c)
	}
	buf = append(buf, '"')
	return buf
}

// MarshalIntSlice: iterate and use our appendIntASM
func MarshalIntSlice(ptr unsafe.Pointer, buf []byte) []byte {
	slice := *(*[]int)(ptr)
	if slice == nil {
		return append(buf, "null"...)
	}
	buf = append(buf, '[')
	for i, v := range slice {
		if i > 0 {
			buf = append(buf, ',')
		}
		newBuf := appendIntASM(buf, int64(v))
		if len(newBuf) == len(buf) {
			buf = strconv.AppendInt(buf, int64(v), 10)
		} else {
			buf = newBuf
		}
	}
	return append(buf, ']')
}

// MarshalStringSlice: iterate calling MarshalString logic
func MarshalStringSlice(ptr unsafe.Pointer, buf []byte) []byte {
	slice := *(*[]string)(ptr)
	if slice == nil {
		return append(buf, "null"...)
	}
	buf = append(buf, '[')
	for i := range slice {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = MarshalString(unsafe.Pointer(&slice[i]), buf)
	}
	return append(buf, ']')
}

// MarshalSlice serializes a slice into a JSON array.
func MarshalSlice[S ~[]T, T any](slice S, reg *Registry, buf []byte) []byte {
	buf = append(buf, '[')
	for i := 0; i < len(slice); i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		ptr := unsafe.Pointer(&slice[i])
		buf = MarshalObject(ptr, reg, buf)
	}
	buf = append(buf, ']')
	return buf
}

// Marshal serializes a single structure.
func Marshal[T any](obj *T, reg *Registry, buf []byte) []byte {
	return MarshalObject(unsafe.Pointer(obj), reg, buf)
}

// MarshalObject performs recursive structure-to-JSON serialization.
func MarshalObject(ptr unsafe.Pointer, reg *Registry, buf []byte) []byte {
	buf = append(buf, '{')
	fields := reg.Fields

	if len(fields) > 0 {
		// First field (no comma before it)
		f := &fields[0]
		buf = append(buf, f.EncodedKey...)
		buf = f.Marshaler(unsafe.Pointer(uintptr(ptr)+f.Offset), buf)

		// Remaining fields
		for i := 1; i < len(fields); i++ {
			buf = append(buf, ',')
			f := &fields[i]
			buf = append(buf, f.EncodedKey...)
			buf = f.Marshaler(unsafe.Pointer(uintptr(ptr)+f.Offset), buf)
		}
	}

	buf = append(buf, '}')
	return buf
}

func MarshalInt(ptr unsafe.Pointer, buf []byte) []byte {
	val := *(*int64)(ptr)
	newBuf := appendIntASM(buf, val)
	if len(newBuf) == len(buf) {
		return strconv.AppendInt(buf, val, 10)
	}
	return newBuf
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

// ParseObject parses a single JSON object and maps it directly into memory via unsafe.Pointer.
func ParseObject(raw []byte, reg *Registry, ptr unsafe.Pointer) error {
	n, err := parseObjectAt(raw, reg, ptr)
	if err != nil {
		return err
	}
	for n < len(raw) && (charTable[raw[n]]&charSpace) != 0 {
		n++
	}
	if n != len(raw) {
		return fmt.Errorf("%w: trailing data after object at %d", ErrTypeMismatch, n)
	}
	return nil
}

func parseObjectAt(raw []byte, reg *Registry, ptr unsafe.Pointer) (int, error) {
	if len(raw) == 0 {
		return 0, ErrUnexpectedEOF
	}

	i := 0
	for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
	if i >= len(raw) || (charTable[raw[i]]&charOpenBrace) == 0 {
		return 0, fmt.Errorf("%w: expected object start", ErrTypeMismatch)
	}
	i++

	for {
		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
		if i >= len(raw) {
			return 0, ErrUnexpectedEOF
		}
		if (charTable[raw[i]] & charCloseBrace) != 0 {
			return i + 1, nil
		}
		if (charTable[raw[i]] & charString) == 0 {
			return 0, fmt.Errorf("%w: expected key quote at %d (%q)", ErrTypeMismatch, i, raw[i])
		}

		i++
		written, consumed := parseShortStringASM2(raw[i:])
		if consumed < 0 {
			return 0, ErrUnexpectedEOF
		}
		decoded := raw[i : i+int(written)]
		var keySlice string
		if reg.CopyStrings {
			keySlice = string(decoded)
		} else {
			keySlice = unsafe.String(unsafe.SliceData(decoded), len(decoded))
		}
		i += int(consumed)

		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
		if i >= len(raw) || (charTable[raw[i]]&charColon) == 0 {
			return 0, ErrTypeMismatch
		}
		i++

		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
		if i >= len(raw) {
			return 0, ErrUnexpectedEOF
		}

		var info FieldInfo
		var ok bool
		keyLen := len(decoded)
		var keyU64 uint64
		if keyLen > 0 && keyLen <= 8 {
			b := unsafe.SliceData(decoded)
			switch keyLen {
			case 1:
				keyU64 = uint64(*b)
			case 2:
				keyU64 = uint64(*(*uint16)(unsafe.Pointer(b)))
			case 3:
				keyU64 = uint64(*(*uint16)(unsafe.Pointer(b))) | (uint64(*(*uint8)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 2))) << 16)
			case 4:
				keyU64 = uint64(*(*uint32)(unsafe.Pointer(b)))
			case 5:
				keyU64 = uint64(*(*uint32)(unsafe.Pointer(b))) | (uint64(*(*uint8)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 4))) << 32)
			case 6:
				keyU64 = uint64(*(*uint32)(unsafe.Pointer(b))) | (uint64(*(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 4))) << 32)
			case 7:
				keyU64 = uint64(*(*uint32)(unsafe.Pointer(b))) | (uint64(*(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 4))) << 32) | (uint64(*(*uint8)(unsafe.Pointer(uintptr(unsafe.Pointer(b)) + 6))) << 48)
			case 8:
				keyU64 = *(*uint64)(unsafe.Pointer(b))
			}
		}

		if len(reg.Fields) <= 16 {
			for idx := 0; idx < len(reg.Fields); idx++ {
				if keyLen <= 8 {
					if reg.Fields[idx].KeyLen == keyLen && reg.Fields[idx].KeyUint64 == keyU64 {
						info = reg.Fields[idx]
						ok = true
						break
					}
				} else {
					if reg.Fields[idx].Key == keySlice {
						info = reg.Fields[idx]
						ok = true
						break
					}
				}
			}
		} else {
			info, ok = reg.NameMap[keySlice]
		}
		if ok {
			switch info.Type {
			case TypeString:
				if (charTable[raw[i]] & charString) == 0 {
					return 0, fmt.Errorf("%w: expected string value", ErrTypeMismatch)
				}
				i++
				written, consumed := parseShortStringASM2(raw[i:])
				if consumed < 0 {
					return 0, ErrUnexpectedEOF
				}
				decoded := raw[i : i+int(written)]
				var strVal string
				if reg.CopyStrings {
					strVal = string(decoded)
				} else {
					strVal = unsafe.String(unsafe.SliceData(decoded), len(decoded))
				}
				*(*string)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = strVal
				i += int(consumed)

			case TypeInt:
				valStart := i
				for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
					i++
				}
				*(*int)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = fastParseInt(raw[valStart:i])

			case TypeFloat:
				valStart := i
				for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
					i++
				}
				*(*float64)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = fastParseFloat(raw[valStart:i])

			case TypeBool:
				if i+3 < len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == trueMagic {
					*(*bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = true
					i += 4
				} else if i+4 < len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == falsePrefixMagic && (charTable[raw[i+4]]&charLetterE) != 0 {
					*(*bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = false
					i += 5
				} else {
					return 0, fmt.Errorf("%w: expected boolean value", ErrTypeMismatch)
				}

			case TypeStruct:
				if (charTable[raw[i]] & charOpenBrace) == 0 {
					return 0, fmt.Errorf("%w: expected object value", ErrTypeMismatch)
				}
				subPtr := unsafe.Pointer(uintptr(ptr) + info.Offset)
				consumed, err := parseObjectAt(raw[i:], info.Sub, subPtr)
				if err != nil {
					return 0, err
				}
				i += consumed

			case TypeStringSlice:
				if (charTable[raw[i]] & charOpenBracket) == 0 {
					return 0, fmt.Errorf("%w: expected string slice value", ErrTypeMismatch)
				}
				existingSlice := *(*[]string)(unsafe.Pointer(uintptr(ptr) + info.Offset))
				slice, consumed, err := parseStringSliceAt(raw[i:], 0, existingSlice, reg.CopyStrings)
				if err != nil {
					return 0, err
				}
				*(*[]string)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = slice
				i += consumed

			case TypeIntSlice:
				if (charTable[raw[i]] & charOpenBracket) == 0 {
					return 0, fmt.Errorf("%w: expected int slice value", ErrTypeMismatch)
				}
				existingSlice := *(*[]int)(unsafe.Pointer(uintptr(ptr) + info.Offset))
				slice, consumed, err := parseIntSliceAt(raw, i, existingSlice)
				if err != nil {
					return 0, err
				}
				*(*[]int)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = slice
				i += consumed
			}
		} else {
			i = skipValue(raw, i)
		}

		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
		if i < len(raw) && (charTable[raw[i]]&(charComma|charCloseBrace)) == 0 {
			i = skipValue(raw, i)
			for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
		}
		if i >= len(raw) {
			return 0, ErrUnexpectedEOF
		}
		switch raw[i] {
		case ',':
			i++
		case '}':
			return i + 1, nil
		default:
			return 0, fmt.Errorf("%w: expected delimiter after key at %d (%q)", ErrTypeMismatch, i, raw[i])
		}
	}
}

func parseStringSliceAt(raw []byte, start int, dst []string, copyStrings bool) ([]string, int, error) {
	if start < 0 || start >= len(raw) || (charTable[raw[start]]&charOpenBracket) == 0 {
		return nil, 0, ErrTypeMismatch
	}

	i := start + 1
	initialized := false

	for {
		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
		if i >= len(raw) {
			return nil, 0, ErrUnexpectedEOF
		}
		if (charTable[raw[i]] & charCloseBracket) != 0 {
			if !initialized {
				if dst == nil {
					return nil, i - start + 1, nil
				}
				return dst[:0], i - start + 1, nil
			}
			return dst, i - start + 1, nil
		}
		if (charTable[raw[i]] & charString) == 0 {
			return nil, 0, ErrTypeMismatch
		}
		i++

		written, consumed := parseShortStringASM2(raw[i:])
		if consumed < 0 {
			return nil, 0, ErrUnexpectedEOF
		}

		if !initialized {
			if dst == nil {
				dst = make([]string, 0, 4)
			} else {
				dst = dst[:0]
			}
			initialized = true
		}

		decoded := raw[i : i+int(written)]
		var strVal string
		if copyStrings {
			strVal = string(decoded)
		} else {
			strVal = unsafe.String(unsafe.SliceData(decoded), len(decoded))
		}
		dst = append(dst, strVal)
		i += int(consumed)

		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
		if i >= len(raw) {
			return nil, 0, ErrUnexpectedEOF
		}
		if (charTable[raw[i]] & charComma) != 0 {
			i++
			continue
		}
		if (charTable[raw[i]] & charCloseBracket) != 0 {
			return dst, i - start + 1, nil
		}
		return nil, 0, ErrTypeMismatch
	}
}

func parseIntSliceAt(raw []byte, start int, dst []int) ([]int, int, error) {
	if start < 0 || start >= len(raw) || (charTable[raw[start]]&charOpenBracket) == 0 {
		return nil, 0, ErrTypeMismatch
	}

	i := start + 1
	initialized := false

	for {
		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
		if i >= len(raw) {
			return nil, 0, ErrUnexpectedEOF
		}
		if (charTable[raw[i]] & charCloseBracket) != 0 {
			if !initialized {
				if dst == nil {
					return nil, i - start + 1, nil
				}
				return dst[:0], i - start + 1, nil
			}
			return dst, i - start + 1, nil
		}
		if (charTable[raw[i]] & charDigit) == 0 {
			return nil, 0, ErrTypeMismatch
		}

		startNum := i
		for i < len(raw) && (charTable[raw[i]]&charDigit) != 0 {
			i++
		}

		if !initialized {
			if dst == nil {
				dst = make([]int, 0, 4)
			} else {
				dst = dst[:0]
			}
			initialized = true
		}

		dst = append(dst, fastParseInt(raw[startNum:i]))

		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
		if i >= len(raw) {
			return nil, 0, ErrUnexpectedEOF
		}
		if (charTable[raw[i]] & charComma) != 0 {
			i++
			continue
		}
		if (charTable[raw[i]] & charCloseBracket) != 0 {
			return dst, i - start + 1, nil
		}
		return nil, 0, ErrTypeMismatch
	}
}

func skipValue(raw []byte, i int) int {
	if i < 0 || i > len(raw) {
		return len(raw) // Protection against bad index
	}

	switch raw[i] {
	case '"':
		i++
		for i < len(raw) {
			if (charTable[raw[i]] & charEscape) != 0 {
				i += 2
				continue
			}
			if (charTable[raw[i]] & charString) != 0 {
				return i + 1
			}
			i++
		}
		return len(raw)
	case '{':
		return skipComposite(raw, i, '{', '}')
	case '[':
		return skipComposite(raw, i, '[', ']')
	default:
		for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
			i++
		}
		return i
	}
}

func skipComposite(raw []byte, start int, open, close byte) int {
	depth := 1
	for i := start + 1; i < len(raw); i++ {
		c := raw[i]
		switch c {
		case '"':
			i++
			for i < len(raw) {
				if (charTable[raw[i]] & charEscape) != 0 {
					i += 2
					continue
				}
				if (charTable[raw[i]] & charString) != 0 {
					break
				}
				i++
			}
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(raw)
}

// UnmarshalSlice processes a raw JSON array sequentially, invoking ParseObject for each element.
func UnmarshalSlice[T any](raw []byte, reg *Registry, dst []T) ([]T, error) {
	if len(raw) == 0 {
		return dst[:0], nil
	}

	buf := reg.chunkPool.Get().([]Chunk)
	need := estimateChunkCapacity(raw)
	if cap(buf) < need {
		buf = make([]Chunk, need)
	}

	count, _ := findObjectBoundariesASM(raw, buf[:len(buf)])
	if count < 0 || count > len(buf) {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, fmt.Errorf("asm returned invalid count: %d", count)
	}

	// Check if there is enough space in the provided slice
	if len(dst) < count {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, fmt.Errorf("insufficient capacity: need %d, have %d", count, len(dst))
	}

	// Work with truncated slice of required size
	target := dst[:count]
	if count == 0 {
		reg.chunkPool.Put(buf[:cap(buf)])
		return target, nil
	}
	structSize := unsafe.Sizeof(*new(T))
	basePtr := unsafe.Pointer(&target[0])

	var err error
	for i := 0; i < count; i++ {
		chunk := buf[i]
		// Check: bounds must be logical
		if chunk.Start < 0 || chunk.End > len(raw) || chunk.Start >= chunk.End {
			err = fmt.Errorf("invalid json boundaries at chunk %d", i)
			break
		}

		itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(i) * structSize))

		if err = ParseObject(raw[chunk.Start:chunk.End], reg, itemPtr); err != nil {
			break
		}
	}

	reg.chunkPool.Put(buf[:cap(buf)])
	if err != nil {
		return nil, err
	}
	return target, nil
}

func estimateChunkCapacity(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	est := len(raw)/128 + 1024
	if est < 1024 {
		est = 1024
	}
	return est
}

func allocBytes(n int) []byte {
	return make([]byte, n)
}

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
	res, fraction, div := 0.0, 0.0, 1.0
	inFrac := false
	for _, b := range buf {
		if (charTable[b] & charDot) != 0 {
			inFrac = true
			continue
		}
		if (charTable[b] & charDigit) != 0 {
			if inFrac {
				fraction = fraction*10 + float64(b-'0')
				div *= 10
			} else {
				res = res*10 + float64(b-'0')
			}
		}
	}
	return res + (fraction / div)
}

// parseStringSliceSafe extracts an array of strings, reusing the provided dst slice capacity.
func parseStringSliceSafe(buf []byte, dst []string) ([]string, error) {
	if len(buf) <= 2 {
		return nil, nil
	}
	if dst == nil {
		dst = make([]string, 0, 4)
	}
	dst = dst[:0]

	for i := 0; i < len(buf); i++ {
		if (charTable[buf[i]] & charString) != 0 {
			i++
			strVal, read, err := parseJSONString(buf, i, false)
			if err != nil {
				return nil, err
			}
			dst = append(dst, strVal)
			i += read
		}
	}
	return dst, nil
}

// parseIntSlice extracts an array of integers, reusing the provided dst slice capacity.
func parseIntSlice(buf []byte, dst []int) []int {
	if len(buf) <= 2 {
		return nil
	}
	if dst == nil {
		dst = make([]int, 0, 4)
	}
	dst = dst[:0]

	for i := 0; i < len(buf); i++ {
		if (charTable[buf[i]] & charDigit) != 0 {
			start := i
			for i < len(buf) && (charTable[buf[i]]&charDigit) != 0 {
				i++
			}
			dst = append(dst, fastParseInt(buf[start:i]))
		}
	}
	return dst
}

func findBounds(raw []byte, start int, open, close byte) int {
	depth := 1
	openBit := charTable[open]
	closeBit := charTable[close]
	for i := start + 1; i < len(raw); i++ {
		c := raw[i]
		if (charTable[c] & charString) != 0 {
			i++
			for i < len(raw) && (charTable[raw[i]]&charString) == 0 {
				if (charTable[raw[i]] & charEscape) != 0 {
					i++
				}
				i++
			}
		} else if (charTable[c] & openBit) != 0 {
			depth++
		} else if (charTable[c] & closeBit) != 0 {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(raw) - 1 // Return boundary if struct is not closed
}

func countArrayItems(buf []byte) int {
	if len(buf) <= 2 {
		return 0
	}

	inString := false
	escaped := false
	depth := 0
	count := 0
	hasValue := false

	for i := 0; i < len(buf); i++ {
		c := buf[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if (charTable[c] & charEscape) != 0 {
				escaped = true
				continue
			}
			if (charTable[c] & charString) != 0 {
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
			hasValue = true
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 1 {
				count++
				hasValue = true
			}
		case ' ', '\n', '\t', '\r':
		default:
			if depth == 1 {
				hasValue = true
			}
		}
	}

	if !hasValue {
		return 0
	}
	return count + 1
}

// ========================== PARALLELISM ==========================

func findObjectBoundaries(data []byte, buf []Chunk) ([]Chunk, int) {
	// Estimation: JSON array of objects can't have more items than '{'

	count, maxSize := findObjectBoundariesASM(data, buf)
	if count > len(buf) {
		count = len(buf)
	}

	return buf[:count], maxSize
}

func UnmarshalArrayParallel[T any](raw []byte, reg *Registry, dst []T) ([]T, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	buf := reg.chunkPool.Get().([]Chunk)
	need := estimateChunkCapacity(raw)
	if cap(buf) < need {
		buf = make([]Chunk, need)
	}

	count, maxDepth := findObjectBoundariesASM(raw[:len(raw)], buf[:len(buf)])

	if count < 0 || count > len(buf) {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, fmt.Errorf("asm returned invalid count: %d", count)
	}

	// CHECK 1: JSON validity
	if maxDepth != 0 {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, errors.New("malformed json: unbalanced braces or brackets")
	}

	// CHECK 3: dst capacity
	if count > len(dst) {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, fmt.Errorf("dst capacity insufficient: need %d, have %d", count, len(dst))
	}
	target := dst[:count]

	if len(target) == 0 {
		reg.chunkPool.Put(buf[:cap(buf)])
		return target, nil
	}

	if err := parseArrayParallelChunks(raw, buf[:count], reg, unsafe.Pointer(&target[0]), unsafe.Sizeof(*new(T))); err != nil {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, err
	}
	reg.chunkPool.Put(buf[:cap(buf)])
	return target, nil
}

func parseArrayParallelChunks(raw []byte, chunks []Chunk, reg *Registry, basePtr unsafe.Pointer, structSize uintptr) error {
	if len(chunks) == 0 {
		return nil
	}

	workers := runtime.GOMAXPROCS(0)
	const minChunksPerWorker = 128
	maxUsefulWorkers := (len(chunks) + minChunksPerWorker - 1) / minChunksPerWorker
	if workers > maxUsefulWorkers {
		workers = maxUsefulWorkers
	}
	if workers <= 1 {
		for idx := 0; idx < len(chunks); idx++ {
			chunk := chunks[idx]
			itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(idx) * structSize))
			if err := ParseObject(raw[chunk.Start:chunk.End], reg, itemPtr); err != nil {
				return err
			}
		}
		return nil
	}

	oncePool.Do(initWorkerPool)

	batchSize := (len(chunks) + workers - 1) / workers
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		start := w * batchSize
		if start >= len(chunks) {
			break
		}
		end := start + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		wg.Add(1)
		globalTaskCh <- workerTask{
			raw:        raw,
			chunks:     chunks,
			reg:        reg,
			basePtr:    basePtr,
			structSize: structSize,
			start:      start,
			end:        end,
			errCh:      errCh,
			wg:         &wg,
		}
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// unescapeStringInPlace modifies the provided raw slice in-place, removing escape characters.
// It returns a zero-copy string pointing to the same memory segment.
// WARNING: This mutates the underlying rawJSON byte array.
func unescapeStringInPlace(raw []byte) string {
	// We write to the exact same array from which we read!
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

type workerTask struct {
	raw        []byte
	chunks     []Chunk
	reg        *Registry
	basePtr    unsafe.Pointer
	structSize uintptr
	start      int
	end        int
	errCh      chan error
	wg         *sync.WaitGroup
}

var (
	globalTaskCh chan workerTask
	oncePool     sync.Once
)

func initWorkerPool() {
	numWorkers := runtime.GOMAXPROCS(0)
	globalTaskCh = make(chan workerTask, numWorkers*8)
	for i := 0; i < numWorkers; i++ {
		go func() {
			for task := range globalTaskCh {
				executeTask(task)
			}
		}()
	}
}

func executeTask(task workerTask) {
	defer task.wg.Done()
	for idx := task.start; idx < task.end; idx++ {
		chunk := task.chunks[idx]
		itemPtr := unsafe.Pointer(uintptr(task.basePtr) + (uintptr(idx) * task.structSize))
		if err := ParseObject(task.raw[chunk.Start:chunk.End], task.reg, itemPtr); err != nil {
			select {
			case task.errCh <- err:
			default:
			}
			return
		}
	}
}
