package silentjson

import (
	"fmt"
	"reflect"
	"strings"
	"unsafe"
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
	if reg.buildErr != nil {
		// P0-2 fix: fail fast instead of calling a nil Marshaler.
		// Use reg.Err() to check the registry before calling Marshal.
		panic("silentjson: cannot marshal " + reg.typeName + ": " + reg.buildErr.Error())
	}

	if ptr == nil {
		return append(buf, "null"...)
	}

	buf = append(buf, '{')
	fields := reg.Fields

	first := true
	for i := 0; i < len(fields); i++ {
		f := &fields[i]
		ptrNew := unsafe.Pointer(uintptr(ptr) + f.Offset)

		// Check OmitEmpty before adding anything to buf
		if f.OmitEmpty {
			// We need a temporary check for empty values
			// Since we don't have a universal IsEmpty, we rely on the marshaler to skip if OmitEmpty is true
			// But the current field marshaler for OmitEmpty returns buf as-is if empty.

			// Let's use a small temporary buffer to check if it's empty
			tmpBuf := f.Marshaler(ptrNew, nil)
			if len(tmpBuf) == 0 {
				continue
			}

			if !first {
				buf = append(buf, ',')
			}
			buf = append(buf, f.EncodedKey...)
			buf = append(buf, tmpBuf...)
		} else {
			if !first {
				buf = append(buf, ',')
			}
			buf = append(buf, f.EncodedKey...)
			buf = f.Marshaler(ptrNew, buf)
		}
		first = false
	}

	buf = append(buf, '}')
	return buf
}

func parseRootSlice(raw []byte, reg *Registry, ptr unsafe.Pointer, copyStrings bool) error {
	// Здесь ptr трактуется как указатель на слайс: *[]T
	// Нам потребуются метаданные из reg.SliceType и reg.ElemType.
	if reg.SliceType == nil || reg.ElemType == nil {
		return fmt.Errorf("registry missing slice type metadata for root slice parsing")
	}

	// Логика аналогична TypeStructSlice из parseObjectAt:
	header := (*reflect.SliceHeader)(ptr)
	prevLen := header.Len
	if header.Data == 0 || header.Cap == 0 {
		newSlice := reflect.MakeSlice(reg.SliceType, 0, 16)
		// Typed Set keeps the backing array alive for the GC
		reflect.NewAt(reg.SliceType, ptr).Elem().Set(newSlice)
	}
	header.Len = 0
	elemSize := reg.ElemType.Size()

	i := 1 // пропускаем '['
	for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
	if i < len(raw) && raw[i] == ']' {
		i++
		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
			i++
		}
		if i != len(raw) {
			return fmt.Errorf("%w: trailing data after root array at %d", ErrTypeMismatch, i)
		}
		return nil
	}

	for {
		if header.Len >= header.Cap {
			newCap := header.Cap * 2
			oldSlice := reflect.NewAt(reg.SliceType, ptr).Elem()
			newSlice := reflect.MakeSlice(reg.SliceType, header.Len, newCap)
			reflect.Copy(newSlice, oldSlice)
			// Typed Set keeps the backing array alive for the GC
			oldSlice.Set(newSlice)
		}

		base := unsafe.Pointer(header.Data)
		slotPtr := unsafe.Pointer(uintptr(base) + uintptr(header.Len)*elemSize)

		// Handle null elements in pointer-bearing slices
		if reg.ElemType.Kind() == reflect.Ptr && i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
			*(*unsafe.Pointer)(slotPtr) = nil
			header.Len++
			i += 4
		} else {
			var elemPtr unsafe.Pointer
			if reg.ElemType.Kind() == reflect.Ptr {
				nv := reflect.New(reg.ElemType.Elem())
				elemPtr = nv.UnsafePointer()
				*(*unsafe.Pointer)(slotPtr) = elemPtr
			} else {
				elemPtr = slotPtr
				b := unsafe.Slice((*byte)(elemPtr), elemSize)
				for j := range b {
					b[j] = 0
				}
			}

			consumed, err := parseObjectAt(raw[i:], reg.ElemSub, elemPtr, copyStrings)
				if err != nil {
					// Clear uncommitted pointer and stale tail on error
					if reg.ElemType.Kind() == reflect.Ptr && header.Data != 0 {
						*(*unsafe.Pointer)(slotPtr) = nil
						for j := header.Len + 1; j < prevLen; j++ {
							staleSlot := unsafe.Pointer(uintptr(base) + uintptr(j)*elemSize)
							*(*unsafe.Pointer)(staleSlot) = nil
						}
					}
					return err
				}
				header.Len++
				i += consumed
			}

		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(raw) {
			// Clear uncommitted pointer and stale tail on EOF error
			if reg.ElemType.Kind() == reflect.Ptr && header.Data != 0 {
				slotPtr := unsafe.Pointer(uintptr(base) + uintptr(header.Len)*elemSize)
				*(*unsafe.Pointer)(slotPtr) = nil
				for j := header.Len + 1; j < prevLen; j++ {
					staleSlot := unsafe.Pointer(uintptr(base) + uintptr(j)*elemSize)
					*(*unsafe.Pointer)(staleSlot) = nil
				}
			}
			return ErrUnexpectedEOF
		}
		if raw[i] == ',' {
			i++
			for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
				i++
			}
			} else if raw[i] == ']' {
				i++
				for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
					i++
				}
				if i != len(raw) {
					return fmt.Errorf("%w: trailing data after root array at %d", ErrTypeMismatch, i)
				}
				// Clear stale pointers in reused backing array for pointer-bearing slices
				if reg.ElemType.Kind() == reflect.Ptr && header.Len < prevLen {
					base := unsafe.Pointer(header.Data)
					for j := header.Len; j < prevLen; j++ {
						slot := unsafe.Pointer(uintptr(base) + uintptr(j)*elemSize)
						*(*unsafe.Pointer)(slot) = nil
					}
				}
				return nil
			} else {
				// Clear uncommitted pointer and stale tail on unexpected char error
				if reg.ElemType.Kind() == reflect.Ptr && header.Data != 0 {
					slotPtr := unsafe.Pointer(uintptr(base) + uintptr(header.Len)*elemSize)
					*(*unsafe.Pointer)(slotPtr) = nil
					for j := header.Len + 1; j < prevLen; j++ {
						staleSlot := unsafe.Pointer(uintptr(base) + uintptr(j)*elemSize)
						*(*unsafe.Pointer)(staleSlot) = nil
					}
				}
				return fmt.Errorf("unexpected char in root slice parsing")
			}
	}
}

// ParseObject parses a single JSON object and maps it directly into memory via unsafe.Pointer.
// Strings are zero-copy aliased to raw unless reg.CopyStrings is set.
func ParseObject(raw []byte, reg *Registry, ptr unsafe.Pointer) error {
	return parseObjectStrict(raw, reg, ptr, reg.CopyStrings)
}

func parseObjectStrict(raw []byte, reg *Registry, ptr unsafe.Pointer, copyStrings bool) error {
	if reg.buildErr != nil {
		return fmt.Errorf("silentjson: registry for %s has unsupported fields: %w", reg.typeName, reg.buildErr)
	}

	i := 0
	for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}

	if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
		// Root null: for slices, set to nil; for objects, no-op like encoding/json.
		i += 4
		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
			i++
		}
		if i != len(raw) {
			return fmt.Errorf("%w: trailing data after root null at %d", ErrTypeMismatch, i)
		}
		if reg.SliceType != nil && reg.ElemType != nil {
			// Slice parse: set the slice to nil
			reflect.NewAt(reg.SliceType, ptr).Elem().SetZero()
		}
		return nil
	}

	if i < len(raw) && raw[i] == '[' {
		// Мы обнаружили массив на верхнем уровне!
		return parseRootSlice(raw[i:], reg, ptr, copyStrings)
	}

	elemReg := reg
	if reg.IsSlice && reg.ElemSub != nil {
		// Element registry for UnmarshalSlice-style calls with a root slice registry
		elemReg = reg.ElemSub
	}

	n, err := parseObjectAt(raw, elemReg, ptr, copyStrings)
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

// jsonValueStart reports whether c can begin a JSON value.

// consumeLiteral validates that raw[i:] starts with the exact JSON
// literal (true/false/null) and returns the position right after it.
func consumeLiteral(raw []byte, i int) (int, error) {
	var want string
	switch raw[i] {
	case 't':
		want = "true"
	case 'f':
		want = "false"
	case 'n':
		want = "null"
	default:
		return 0, fmt.Errorf("%w: invalid literal start %q at %d", ErrTypeMismatch, raw[i], i)
	}
	if i+len(want) > len(raw) || string(raw[i:i+len(want)]) != want {
		return 0, fmt.Errorf("%w: invalid literal at %d", ErrTypeMismatch, i)
	}
	return i + len(want), nil
}

// skipValueStrict skips exactly one JSON value, validating its grammar
// the way encoding/json does: exact literals, strict numbers, escaped
// strings with paired escapes, balanced composites with well-formed
// elements. Used for unknown fields (and RawMessage) where the value is
// not decoded into a field and would otherwise be swallowed unchecked.
// Raw control characters inside strings remain accepted — that is the
// documented laxness of this parser and applies to known fields alike.
func skipValueStrict(raw []byte, i int) (int, error) {
	if i >= len(raw) {
		return 0, ErrUnexpectedEOF
	}
	switch raw[i] {
	case '"':
		// readJSONString expects input starting right AFTER the opening
		// quote; the consumed count includes the closing quote.
		_, _, consumed, err := readJSONString(raw[i+1:], false)
		if err != nil {
			return 0, err
		}
		return i + 1 + int(consumed), nil
	case '{':
		i++
		i = skipSpaceRaw(raw, i)
		if i >= len(raw) {
			return 0, ErrUnexpectedEOF
		}
		if raw[i] == '}' {
			return i + 1, nil
		}
		for {
			if raw[i] != '"' {
				return 0, fmt.Errorf("%w: expected object key at %d", ErrTypeMismatch, i)
			}
			_, _, consumed, err := readJSONString(raw[i+1:], false)
			if err != nil {
				return 0, err
			}
			i += 1 + int(consumed)
			i = skipSpaceRaw(raw, i)
			if i >= len(raw) || raw[i] != ':' {
				return 0, fmt.Errorf("%w: expected ':' at %d", ErrTypeMismatch, i)
			}
			i++
			i = skipSpaceRaw(raw, i)
			i, err = skipValueStrict(raw, i)
			if err != nil {
				return 0, err
			}
			i = skipSpaceRaw(raw, i)
			if i >= len(raw) {
				return 0, ErrUnexpectedEOF
			}
			if raw[i] == ',' {
				i++
				i = skipSpaceRaw(raw, i)
				if i >= len(raw) {
					return 0, ErrUnexpectedEOF
				}
				continue
			}
			if raw[i] == '}' {
				return i + 1, nil
			}
			return 0, fmt.Errorf("%w: expected ',' or '}' at %d", ErrTypeMismatch, i)
		}
	case '[':
		i++
		i = skipSpaceRaw(raw, i)
		if i >= len(raw) {
			return 0, ErrUnexpectedEOF
		}
		if raw[i] == ']' {
			return i + 1, nil
		}
		for {
			// plain '=' with a pre-declared err: ':=' would shadow the
			// loop's i and spin forever on multi-element arrays
			var err error
			i, err = skipValueStrict(raw, i)
			if err != nil {
				return 0, err
			}
			i = skipSpaceRaw(raw, i)
			if i >= len(raw) {
				return 0, ErrUnexpectedEOF
			}
			if raw[i] == ',' {
				i++
				i = skipSpaceRaw(raw, i)
				if i >= len(raw) {
					return 0, ErrUnexpectedEOF
				}
				continue
			}
			if raw[i] == ']' {
				return i + 1, nil
			}
			return 0, fmt.Errorf("%w: expected ',' or ']' at %d", ErrTypeMismatch, i)
		}
	case 't', 'f', 'n':
		return consumeLiteral(raw, i)
	default:
		start := i
		for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
			i++
		}
		if !validJSONNumber(raw[start:i]) {
			// P1-B: a token cut off by the end of the input is an
			// unexpected EOF, not a grammar error — encoding/json
			// reports exactly these shapes as "unexpected end of JSON
			// input". With a delimiter following, the grammar did
			// break: keep the type mismatch.
			if i >= len(raw) && incompleteNumber(raw[start:i]) {
				return 0, fmt.Errorf("%w: truncated number %q at %d", ErrUnexpectedEOF, raw[start:i], start)
			}
			return 0, fmt.Errorf("%w: invalid number %q at %d", ErrTypeMismatch, raw[start:i], start)
		}
		return i, nil
	}
}

func skipSpaceRaw(raw []byte, i int) int {
	for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
		i++
	}
	return i
}

func jsonValueStart(c byte) bool {
	switch c {
	case '"', '-', '{', '[', 't', 'f', 'n':
		return true
	}
	return c >= '0' && c <= '9'
}

func parseObjectAt(raw []byte, reg *Registry, ptr unsafe.Pointer, copyStrings bool) (int, error) {
	if reg.buildErr != nil {
		return 0, fmt.Errorf("silentjson: registry for %s has unsupported fields: %w", reg.typeName, reg.buildErr)
	}
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

	afterComma := false
	for {
		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(raw) {
			return 0, ErrUnexpectedEOF
		}
		if (charTable[raw[i]] & charCloseBrace) != 0 {
			if afterComma {
				return 0, fmt.Errorf("%w: unexpected trailing comma in object at %d", ErrTypeMismatch, i)
			}
			return i + 1, nil
		}
		if afterComma && (charTable[raw[i]]&charString) == 0 {
			return 0, fmt.Errorf("%w: expected key after comma at %d (%q)", ErrTypeMismatch, i, raw[i])
		}
		afterComma = false
		if (charTable[raw[i]] & charString) == 0 {
			return 0, fmt.Errorf("%w: expected key quote at %d (%q)", ErrTypeMismatch, i, raw[i])
		}

		i++
		// Keys are parsed with copyStrings=false ALWAYS: the key string is
		// transient (hash lookup, case fallback, error text) and never
		// stored in the destination, so the zero-copy alias into the input
		// buffer is safe and saves an allocation per key — in stream mode
		// that is ~9 of the ~12.5 per-record allocations. Values keep the
		// caller's copyStrings semantics.
		keySlice, keyBytes, consumed, err := readJSONString(raw[i:], false)
		if err != nil {
			return 0, err
		}
		decoded := keyBytes
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
		if !ok && len(reg.lowerMap) > 0 {
			// encoding/json compat: case-insensitive key fallback (P2-2)
			if ciIdx, ciOk := reg.lowerMap[strings.ToLower(keySlice)]; ciOk {
				info = reg.Fields[ciIdx]
				ok = true
			}
		}
		if ok {
			switch info.Type {
			case TypeString:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					// null leaves the zero value, like encoding/json
					i += 4
					break
				}
				if (charTable[raw[i]] & charString) == 0 {
					return 0, fmt.Errorf("%w: expected string value", ErrTypeMismatch)
				}
				i++
				strVal, _, consumed, err := readJSONString(raw[i:], copyStrings)
				if err != nil {
					return 0, err
				}
				*(*string)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = strVal
				i += int(consumed)

			case TypeInt, TypeUint:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					// null leaves the zero value, like encoding/json
					i += 4
					break
				}
				valStart := i
				for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
					i++
				}
				val, ok := parseJSONInt(raw[valStart:i])
				if !ok {
					return 0, fmt.Errorf("%w: invalid integer %q for field %q", ErrTypeMismatch, raw[valStart:i], info.Key)
				}
				if !intFitsWidth(val, info.Bits, info.Unsigned) {
					return 0, fmt.Errorf("%w: number %d out of range for field %q", ErrTypeMismatch, val, info.Key)
				}
				storeIntWidth(unsafe.Pointer(uintptr(ptr)+info.Offset), val, info.Bits, info.Unsigned)

			case TypeFloat:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					i += 4
					break
				}
				valStart := i
				for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
					i++
				}
				fval, ok, trunc := parseJSONFloat(raw[valStart:i])
				if !ok {
					if trunc && i >= len(raw) {
						// P1-B: the token ran into the end of the input mid-grammar.
						// Keep the value parsed so far, then report the EOF — the
						// fields decoded before the cut stay populated (the same
						// contract as the stream tail: data first, sticky error).
						dst := unsafe.Pointer(uintptr(ptr) + info.Offset)
						if info.Bits == 32 {
							*(*float32)(dst) = float32(fval)
						} else {
							*(*float64)(dst) = fval
						}
						return 0, fmt.Errorf("%w: truncated number %q for field %q", ErrUnexpectedEOF, raw[valStart:i], info.Key)
					}
					return 0, fmt.Errorf("%w: invalid number %q for field %q", ErrTypeMismatch, raw[valStart:i], info.Key)
				}
				dst := unsafe.Pointer(uintptr(ptr) + info.Offset)
				// P0-1 fix: write the field at its real width
				if info.Bits == 32 {
					*(*float32)(dst) = float32(fval)
				} else {
					*(*float64)(dst) = fval
				}

			case TypeBool:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					*(*bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = false
					i += 4
				} else if i+3 < len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == trueMagic {
					*(*bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = true
					i += 4
				} else if i+4 < len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == falsePrefixMagic && (charTable[raw[i+4]]&charLetterE) != 0 {
					*(*bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = false
					i += 5
				} else {
					return 0, fmt.Errorf("%w: expected boolean value", ErrTypeMismatch)
				}

			case TypeStruct:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					// null leaves the zero value, like encoding/json
					i += 4
					break
				}
				if (charTable[raw[i]] & charOpenBrace) == 0 {
					return 0, fmt.Errorf("%w: expected object value", ErrTypeMismatch)
				}
				subPtr := unsafe.Pointer(uintptr(ptr) + info.Offset)
				consumed, err := parseObjectAt(raw[i:], info.Sub, subPtr, copyStrings)
				if err != nil {
					return 0, err
				}
				i += consumed

			case TypeStringSlice:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					*(*[]string)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = nil
					i += 4
					break
				}
				if (charTable[raw[i]] & charOpenBracket) == 0 {
					return 0, fmt.Errorf("%w: expected string slice value", ErrTypeMismatch)
				}
				existingSlice := *(*[]string)(unsafe.Pointer(uintptr(ptr) + info.Offset))
				slice, consumed, err := parseStringSliceAt(raw[i:], 0, existingSlice, copyStrings)
				if err != nil {
					return 0, err
				}
				*(*[]string)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = slice
				i += consumed

			case TypeIntSlice:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					*(*[]int)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = nil
					i += 4
					break
				}
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

			case TypeIntPtr:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					*(*unsafe.Pointer)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = nil
					i += 4
				} else {
					valStart := i
					for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
						i++
					}
					val, ok := parseJSONInt(raw[valStart:i])
					if !ok {
						return 0, fmt.Errorf("%w: invalid integer %q for field %q", ErrTypeMismatch, raw[valStart:i], info.Key)
					}
					if !intFitsWidth(val, info.Bits, info.Unsigned) {
						return 0, fmt.Errorf("%w: number %d out of range for field %q", ErrTypeMismatch, val, info.Key)
					}
					fieldPtr := unsafe.Pointer(uintptr(ptr) + info.Offset)
					elemPtr := *(*unsafe.Pointer)(fieldPtr)
					if elemPtr == nil {
						nv := reflect.New(info.ElemType)
						elemPtr = nv.UnsafePointer()
						*(*unsafe.Pointer)(fieldPtr) = elemPtr
					}
					storeIntWidth(elemPtr, val, info.Bits, info.Unsigned)
				}

			case TypeStringPtr:
				if i < len(raw) && i+3 < len(raw) && raw[i] == 'n' && raw[i+1] == 'u' && raw[i+2] == 'l' && raw[i+3] == 'l' {
					*(**string)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = nil
					i += 4
				} else {
					if (charTable[raw[i]] & charString) == 0 {
						return 0, fmt.Errorf("%w: expected string value", ErrTypeMismatch)
					}
					i++
					strVal, _, consumed, err := readJSONString(raw[i:], copyStrings)
					if err != nil {
						return 0, err
					}
					ptrPtr := (**string)(unsafe.Pointer(uintptr(ptr) + info.Offset))
					if *ptrPtr == nil {
						*ptrPtr = new(string)
					}
					**ptrPtr = strVal
					i += int(consumed)
				}

			case TypeBoolPtr:
				if i < len(raw) && i+3 < len(raw) && raw[i] == 'n' && raw[i+1] == 'u' && raw[i+2] == 'l' && raw[i+3] == 'l' {
					*(**bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = nil
					i += 4
				} else {
					var val bool
					if i+3 < len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == trueMagic {
						val = true
						i += 4
					} else if i+4 < len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == falsePrefixMagic && (charTable[raw[i+4]]&charLetterE) != 0 {
						val = false
						i += 5
					} else {
						return 0, fmt.Errorf("%w: expected boolean value", ErrTypeMismatch)
					}
					ptrPtr := (**bool)(unsafe.Pointer(uintptr(ptr) + info.Offset))
					if *ptrPtr == nil {
						*ptrPtr = new(bool)
					}
					**ptrPtr = val
				}

			case TypeFloatPtr:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					*(*unsafe.Pointer)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = nil
					i += 4
				} else {
					valStart := i
					for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
						i++
					}
					fval, ok, trunc := parseJSONFloat(raw[valStart:i])
					if !ok {
						if trunc && i >= len(raw) {
							// P1-B: keep the partial value — allocating the pointee
							// like the success path — then report the EOF.
							fieldPtr := unsafe.Pointer(uintptr(ptr) + info.Offset)
							elemPtr := *(*unsafe.Pointer)(fieldPtr)
							if elemPtr == nil {
								nv := reflect.New(info.ElemType)
								elemPtr = nv.UnsafePointer()
								*(*unsafe.Pointer)(fieldPtr) = elemPtr
							}
							if info.Bits == 32 {
								*(*float32)(elemPtr) = float32(fval)
							} else {
								*(*float64)(elemPtr) = fval
							}
							return 0, fmt.Errorf("%w: truncated number %q for field %q", ErrUnexpectedEOF, raw[valStart:i], info.Key)
						}
						return 0, fmt.Errorf("%w: invalid number %q for field %q", ErrTypeMismatch, raw[valStart:i], info.Key)
					}
					fieldPtr := unsafe.Pointer(uintptr(ptr) + info.Offset)
					elemPtr := *(*unsafe.Pointer)(fieldPtr)
					if elemPtr == nil {
						nv := reflect.New(info.ElemType)
						elemPtr = nv.UnsafePointer()
						*(*unsafe.Pointer)(fieldPtr) = elemPtr
					}
					// P0-1 fix: write at the pointer's real element width
					if info.Bits == 32 {
						*(*float32)(elemPtr) = float32(fval)
					} else {
						*(*float64)(elemPtr) = fval
					}
				}

			case TypeStructSlice:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					// Typed zero-value assignment sets the slice to nil
					fieldPtr := unsafe.Pointer(uintptr(ptr) + info.Offset)
					reflect.NewAt(info.SliceType, fieldPtr).Elem().SetZero()
					i += 4
					break
				}
				if (charTable[raw[i]] & charOpenBracket) == 0 {
					return 0, fmt.Errorf("%w: expected struct slice value", ErrTypeMismatch)
				}
				i++
				for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
					i++
				}

				header := (*reflect.SliceHeader)(unsafe.Pointer(uintptr(ptr) + info.Offset))
				prevLen := header.Len
				if header.Data == 0 || header.Cap == 0 {
					newSlice := reflect.MakeSlice(info.SliceType, 0, 16)
					// Typed Set keeps the backing array alive for the GC
					reflect.NewAt(info.SliceType, unsafe.Pointer(header)).Elem().Set(newSlice)
				}
				header.Len = 0
				elemSize := info.ElemType.Size()

					if i < len(raw) && raw[i] == ']' {
						// Empty array: truncate the slice
						i++
					} else {
						for {
							if header.Len >= header.Cap {
								newCap := header.Cap * 2
								if newCap == 0 {
									newCap = 16
								}
								oldSlice := reflect.NewAt(info.SliceType, unsafe.Pointer(header)).Elem()
								newSlice := reflect.MakeSlice(info.SliceType, header.Len, newCap)
								reflect.Copy(newSlice, oldSlice)
								// Typed Set keeps the backing array alive for the GC
								oldSlice.Set(newSlice)
							}

							base := unsafe.Pointer(header.Data)
							slotPtr := unsafe.Pointer(uintptr(base) + uintptr(header.Len)*elemSize)

							// Handle null elements in pointer-bearing slices
							if info.ElemType.Kind() == reflect.Ptr && i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
								*(*unsafe.Pointer)(slotPtr) = nil
								header.Len++
								i += 4
							} else {
								var elemPtr unsafe.Pointer
								if info.ElemType.Kind() == reflect.Ptr {
									nv := reflect.New(info.ElemType.Elem())
									elemPtr = nv.UnsafePointer()
									*(*unsafe.Pointer)(slotPtr) = elemPtr
								} else {
									elemPtr = slotPtr
									b := unsafe.Slice((*byte)(elemPtr), elemSize)
									for j := range b {
										b[j] = 0
									}
								}

								consumed, err := parseObjectAt(raw[i:], info.Sub, elemPtr, copyStrings)
						if err != nil {
							// Clear stale pointers before returning on error.
							// Always clear the newly written pointer at header.Len,
							// plus any old pointers from a previous longer slice.
							if info.ElemType.Kind() == reflect.Ptr && header.Data != 0 {
								// Clear the pointer we just wrote (not yet committed)
								slot := unsafe.Pointer(uintptr(base) + uintptr(header.Len)*elemSize)
								*(*unsafe.Pointer)(slot) = nil
								// Clear old stale pointers beyond this one
								for j := header.Len + 1; j < prevLen; j++ {
									slot = unsafe.Pointer(uintptr(base) + uintptr(j)*elemSize)
									*(*unsafe.Pointer)(slot) = nil
								}
							}
							return 0, err
						}
							header.Len++
							i += consumed
						}

						for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
							i++
						}
						if i >= len(raw) {
							// Clear stale pointers on EOF error too
							if info.ElemType.Kind() == reflect.Ptr && header.Data != 0 {
								slot := unsafe.Pointer(uintptr(base) + uintptr(header.Len)*elemSize)
								*(*unsafe.Pointer)(slot) = nil
								for j := header.Len + 1; j < prevLen; j++ {
									slot = unsafe.Pointer(uintptr(base) + uintptr(j)*elemSize)
									*(*unsafe.Pointer)(slot) = nil
								}
							}
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
							// Clear stale pointers on unexpected char error too
							if info.ElemType.Kind() == reflect.Ptr && header.Data != 0 {
								slot := unsafe.Pointer(uintptr(base) + uintptr(header.Len)*elemSize)
								*(*unsafe.Pointer)(slot) = nil
								for j := header.Len + 1; j < prevLen; j++ {
									slot = unsafe.Pointer(uintptr(base) + uintptr(j)*elemSize)
									*(*unsafe.Pointer)(slot) = nil
								}
							}
							return 0, fmt.Errorf("unexpected char in struct slice")
						}
					}
				}

				// Clear stale pointers in reused backing array for pointer-bearing slices.
				// This prevents old objects from remaining reachable by the GC after
				// truncation or replacement with a shorter array.
				if info.ElemType.Kind() == reflect.Ptr && header.Len < prevLen {
					base := unsafe.Pointer(header.Data)
					for j := header.Len; j < prevLen; j++ {
						*(*unsafe.Pointer)(unsafe.Pointer(uintptr(base) + uintptr(j)*elemSize)) = nil
					}
				}

			case TypeStructPtr:
				if i+4 <= len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					*(*unsafe.Pointer)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = nil
					i += 4
				} else {
					if (charTable[raw[i]] & charOpenBrace) == 0 {
						return 0, fmt.Errorf("%w: expected object value", ErrTypeMismatch)
					}
					fieldPtr := unsafe.Pointer(uintptr(ptr) + info.Offset)
					objPtr := *(*unsafe.Pointer)(fieldPtr)
					if objPtr == nil {
						nv := reflect.New(info.ElemType)
						objPtr = nv.UnsafePointer()
						*(*unsafe.Pointer)(fieldPtr) = objPtr
					}
					consumed, err := parseObjectAt(raw[i:], info.Sub, objPtr, copyStrings)
					if err != nil {
						return 0, err
					}
					i += consumed
				}

			case TypeRawMessage:
				end, err := skipValueStrict(raw, i)
				if err != nil {
					return 0, err
				}
				if end > len(raw) {
					end = len(raw)
				}
				if end <= i {
					return 0, fmt.Errorf("%w: empty raw value", ErrTypeMismatch)
				}
				dst := (*RawMessage)(unsafe.Pointer(uintptr(ptr) + info.Offset))
				*dst = append((*dst)[:0], raw[i:end]...)
				i = end

			default:
				return 0, fmt.Errorf("%w: unsupported field kind for key %q", ErrTypeMismatch, keySlice)
			}
		} else {
			if !jsonValueStart(raw[i]) {
				return 0, fmt.Errorf("%w: expected value at %d (%q)", ErrTypeMismatch, i, raw[i])
			}
			end, err := skipValueStrict(raw, i)
			if err != nil {
				return 0, err
			}
			i = end
		}

		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(raw) {
			return 0, ErrUnexpectedEOF
		}
		switch raw[i] {
		case ',':
			i++
			afterComma = true
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
	afterComma := false

	for {
		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(raw) {
			return nil, 0, ErrUnexpectedEOF
		}
		if (charTable[raw[i]] & charCloseBracket) != 0 {
			if afterComma {
				return nil, 0, fmt.Errorf("%w: unexpected trailing comma in array at %d", ErrTypeMismatch, i)
			}
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

		if !initialized {
			if dst == nil {
				dst = make([]string, 0, 4)
			} else {
				dst = dst[:0]
			}
			initialized = true
		}

		strVal, _, consumed, err := readJSONString(raw[i:], copyStrings)
		if err != nil {
			return nil, 0, err
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
			afterComma = true
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
	afterComma := false

	for {
		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(raw) {
			return nil, 0, ErrUnexpectedEOF
		}
		if (charTable[raw[i]] & charCloseBracket) != 0 {
			if afterComma {
				return nil, 0, fmt.Errorf("%w: unexpected trailing comma in array at %d", ErrTypeMismatch, i)
			}
			if !initialized {
				if dst == nil {
					return nil, i - start + 1, nil
				}
				return dst[:0], i - start + 1, nil
			}
			return dst, i - start + 1, nil
		}
		if (charTable[raw[i]]&charDigit) == 0 && raw[i] != '-' {
			return nil, 0, ErrTypeMismatch
		}

		startNum := i
		if raw[i] == '-' {
			i++
		}
		for i < len(raw) && (charTable[raw[i]]&charDigit) != 0 {
			i++
		}
		if i == startNum || (raw[startNum] == '-' && i == startNum+1) {
			return nil, 0, ErrTypeMismatch
		}

		if !initialized {
			if dst == nil {
				dst = make([]int, 0, 4)
			} else {
				dst = dst[:0]
			}
			initialized = true
		}

		val, ok := parseJSONInt(raw[startNum:i])
		if !ok {
			return nil, 0, fmt.Errorf("%w: invalid integer %q", ErrTypeMismatch, raw[startNum:i])
		}
		dst = append(dst, int(val))

		for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(raw) {
			return nil, 0, ErrUnexpectedEOF
		}
		if (charTable[raw[i]] & charComma) != 0 {
			i++
			afterComma = true
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
		if (charTable[buf[i]]&charDigit) != 0 || buf[i] == '-' {
			start := i
			if buf[i] == '-' {
				i++
			}
			for i < len(buf) && (charTable[buf[i]]&charDigit) != 0 {
				i++
			}
			if val, ok := parseJSONInt(buf[start:i]); ok {
				dst = append(dst, int(val))
			}
		}
	}
	return dst
}
