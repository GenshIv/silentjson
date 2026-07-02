package silentjson

import (
	"fmt"
	"reflect"
	"strconv"
	"unsafe"

	"github.com/cespare/ryu"
)

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

	for i := 0; i < len(fields); i++ {
		f := &fields[i]
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, f.EncodedKey...)

		fieldPtr := unsafe.Pointer(uintptr(ptr) + f.Offset)
		switch f.Type {
		case TypeInt:
			val := *(*int64)(fieldPtr)
			newBuf := appendIntASM(buf, val)
			if len(newBuf) == len(buf) {
				buf = strconv.AppendInt(buf, val, 10)
			} else {
				buf = newBuf
			}
		case TypeString:
			s := *(*string)(fieldPtr)
			newBuf, specialPos := appendStringASM(buf, s)
			if specialPos == -1 {
				buf = newBuf
			} else if specialPos == -2 {
				buf = appendJSONStringGo(buf, s)
			} else {
				buf = newBuf
				for j := specialPos; j < len(s); j++ {
					c := s[j]
					if (charTable[c] & (charString | charEscape)) != 0 {
						buf = append(buf, '\\', c)
					} else {
						buf = append(buf, c)
					}
				}
				buf = append(buf, '"')
			}
		case TypeBool:
			if *(*bool)(fieldPtr) {
				buf = append(buf, 't', 'r', 'u', 'e')
			} else {
				buf = append(buf, 'f', 'a', 'l', 's', 'e')
			}
		case TypeFloat:
			buf = ryu.AppendFloat64(buf, *(*float64)(fieldPtr))
		default:
			buf = f.Marshaler(fieldPtr, buf)
		}
	}

	buf = append(buf, '}')
	return buf
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
			var hash uint32
			if keyLen <= 8 {
				hash = hashU64(keyU64)
			} else {
				hash = hashBytes(decoded)
			}
			keysLenPtr := unsafe.Pointer(unsafe.SliceData(reg.HashKeysLen))
			keysU64Ptr := unsafe.Pointer(unsafe.SliceData(reg.HashKeysU64))
			keysStrPtr := unsafe.Pointer(unsafe.SliceData(reg.HashKeysStr))
			valsPtr := unsafe.Pointer(unsafe.SliceData(reg.HashValues))

			idx := hash & reg.HashMask
			for {
				length := int(*(*uint8)(unsafe.Pointer(uintptr(keysLenPtr) + uintptr(idx))))
				if length == 0 {
					break
				}
				if keyLen <= 8 {
					u64Val := *(*uint64)(unsafe.Pointer(uintptr(keysU64Ptr) + uintptr(idx)*8))
					if length == keyLen && u64Val == keyU64 {
						valIdx := *(*int16)(unsafe.Pointer(uintptr(valsPtr) + uintptr(idx)*2))
						info = reg.Fields[valIdx]
						ok = true
						break
					}
				} else {
					strVal := *(*string)(unsafe.Pointer(uintptr(keysStrPtr) + uintptr(idx)*16))
					if strVal == keySlice {
						valIdx := *(*int16)(unsafe.Pointer(uintptr(valsPtr) + uintptr(idx)*2))
						info = reg.Fields[valIdx]
						ok = true
						break
					}
				}
				idx = (idx + 1) & reg.HashMask
			}
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

			case TypeStructSlice:
				if (charTable[raw[i]] & charOpenBracket) == 0 {
					return 0, fmt.Errorf("%w: expected struct slice value", ErrTypeMismatch)
				}
				i++
				for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
					i++
				}
				if i < len(raw) && raw[i] == ']' {
					i++
					continue
				}

				header := (*reflect.SliceHeader)(unsafe.Pointer(uintptr(ptr) + info.Offset))
				if header.Cap == 0 {
					newSlice := reflect.MakeSlice(info.SliceType, 0, 16)
					header.Data = newSlice.Pointer()
					header.Cap = 16
				}
				header.Len = 0
				elemSize := info.ElemType.Size()

				for {
					if header.Len >= header.Cap {
						newCap := header.Cap * 2
						if newCap == 0 {
							newCap = 16
						}
						oldSlice := reflect.NewAt(info.SliceType, unsafe.Pointer(header)).Elem()
						newSlice := reflect.MakeSlice(info.SliceType, header.Len, newCap)
						reflect.Copy(newSlice, oldSlice)
						header.Data = newSlice.Pointer()
						header.Cap = newCap
					}

					elemPtr := unsafe.Pointer(header.Data + uintptr(header.Len)*elemSize)

					// Zero the memory quickly using Go's optimized memclr
					b := unsafe.Slice((*byte)(elemPtr), elemSize)
					for j := range b {
						b[j] = 0
					}

					consumed, err := parseObjectAt(raw[i:], info.Sub, elemPtr)
					if err != nil {
						return 0, err
					}
					header.Len++
					i += consumed

					for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
						i++
					}
					if i >= len(raw) {
						return 0, ErrUnexpectedEOF
					}
					if raw[i] == ',' {
						i++
						for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
							i++
						}
					} else if raw[i] == ']' {
						i++
						break
					} else {
						return 0, fmt.Errorf("unexpected char in struct slice")
					}
				}
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
