package silentjson

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/GenshIv/intHache"
)

var (
	ErrUnexpectedEOF = errors.New("zerojson: unexpected end of JSON input")
	ErrTypeMismatch  = errors.New("zerojson: json type mismatch")
)

type FieldType int

// Precomputed Look-Up Table (LUT) for O(1) character classification
var charTable = [256]byte{
	' ':  charSpace,
	'\n': charSpace,
	'\t': charSpace,
	'\r': charSpace,

	'"':  charString,
	'\\': charEscape,

	'{': charStruct,
	'}': charStruct,
	'[': charStruct,
	']': charStruct,
	':': charStruct,
	',': charStruct,
}

const (
	charNone     byte = 0
	charSpace    byte = 1 << 0 // Spaces, tabs, newlines
	charString   byte = 1 << 1 // Quote "
	charEscape   byte = 1 << 2 // Backslash \
	charStruct   byte = 1 << 3 // Structural characters: { } [ ] : ,
	maskValueEnd      = charStruct | charSpace
)

const (
	nullMagic = uint32(0x6C6C756E) // "null"
	trueMagic = uint32(0x65757274) // "true"
	alseMagic = uint32(0x65736C61) // "alse" (...false)

	TypeInt FieldType = iota
	TypeString
	TypeBool
	TypeFloat
	TypeStruct
	TypeStringSlice
	TypeIntSlice
)

type FieldInfo struct {
	EncodedKey []byte // Pre-serialized key, e.g., `"key":`
	Offset     uintptr
	Type       FieldType
	Sub        *Registry
	OmitEmpty  bool
}

// Registry: Map for parsing lookup, Fields for fast sequential generation
type Registry struct {
	Map    map[int64]FieldInfo
	Fields []FieldInfo
}

// BuildRegistry constructs a registry for a given struct type.
func BuildRegistry(typ reflect.Type) *Registry {
	reg := &Registry{
		Map:    make(map[int64]FieldInfo),
		Fields: make([]FieldInfo, 0, typ.NumField()),
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}

		parts := strings.Split(tag, ",")
		key := parts[0]

		info := FieldInfo{
			EncodedKey: []byte(`"` + key + `":`),
			Offset:     field.Offset,
		}

		for _, opt := range parts[1:] {
			if opt == "omitempty" {
				info.OmitEmpty = true
			}
		}

		switch field.Type.Kind() {
		case reflect.Int, reflect.Int64:
			info.Type = TypeInt
		case reflect.String:
			info.Type = TypeString
		case reflect.Bool:
			info.Type = TypeBool
		case reflect.Float64, reflect.Float32:
			info.Type = TypeFloat
		case reflect.Struct:
			info.Type = TypeStruct
			info.Sub = BuildRegistry(field.Type)
		case reflect.Slice:
			if field.Type.Elem().Kind() == reflect.String {
				info.Type = TypeStringSlice
			} else if field.Type.Elem().Kind() == reflect.Int {
				info.Type = TypeIntSlice
			}
		}

		hashe := intHache.Sum([]byte(key))
		reg.Map[hashe] = info
		reg.Fields = append(reg.Fields, info)
	}
	return reg
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
	first := true

	// Iteration strictly by indices to avoid allocations
	fields := reg.Fields
	for i := 0; i < len(fields); i++ {
		info := &fields[i]

		if info.OmitEmpty {
			isEmpty := false
			switch info.Type {
			case TypeInt:
				isEmpty = *(*int)(unsafe.Pointer(uintptr(ptr) + info.Offset)) == 0
			case TypeString:
				isEmpty = *(*string)(unsafe.Pointer(uintptr(ptr) + info.Offset)) == ""
			case TypeBool:
				isEmpty = !(*(*bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)))
			case TypeFloat:
				isEmpty = *(*float64)(unsafe.Pointer(uintptr(ptr) + info.Offset)) == 0.0
			case TypeStringSlice:
				isEmpty = len(*(*[]string)(unsafe.Pointer(uintptr(ptr) + info.Offset))) == 0
			case TypeIntSlice:
				isEmpty = len(*(*[]int)(unsafe.Pointer(uintptr(ptr) + info.Offset))) == 0
			}
			if isEmpty {
				continue
			}
		}

		if !first {
			buf = append(buf, ',')
		}
		first = false

		buf = append(buf, info.EncodedKey...)

		switch info.Type {
		case TypeInt:
			buf = strconv.AppendInt(buf, int64(*(*int)(unsafe.Pointer(uintptr(ptr) + info.Offset))), 10)
		case TypeFloat:
			buf = strconv.AppendFloat(buf, *(*float64)(unsafe.Pointer(uintptr(ptr) + info.Offset)), 'f', -1, 64)
		case TypeBool:
			buf = strconv.AppendBool(buf, *(*bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)))
		case TypeString:
			buf = strconv.AppendQuote(buf, *(*string)(unsafe.Pointer(uintptr(ptr) + info.Offset)))
		case TypeStruct:
			buf = MarshalObject(unsafe.Pointer(uintptr(ptr)+info.Offset), info.Sub, buf)
		case TypeStringSlice:
			slice := *(*[]string)(unsafe.Pointer(uintptr(ptr) + info.Offset))
			if slice == nil {
				buf = append(buf, "null"...)
			} else {
				buf = append(buf, '[')
				for idx, v := range slice {
					if idx > 0 {
						buf = append(buf, ',')
					}
					buf = strconv.AppendQuote(buf, v)
				}
				buf = append(buf, ']')
			}
		case TypeIntSlice:
			slice := *(*[]int)(unsafe.Pointer(uintptr(ptr) + info.Offset))
			if slice == nil {
				buf = append(buf, "null"...)
			} else {
				buf = append(buf, '[')
				for idx, v := range slice {
					if idx > 0 {
						buf = append(buf, ',')
					}
					buf = strconv.AppendInt(buf, int64(v), 10)
				}
				buf = append(buf, ']')
			}
		}
	}
	buf = append(buf, '}')
	return buf
}

// ParseObject parses a single JSON object and maps it directly into memory via unsafe.Pointer.
func ParseObject(raw []byte, reg *Registry, ptr unsafe.Pointer) error {
	for i, j := 0, 1; i < len(raw); i, j = i+1, j+1 {
		// Identify the start of the key (LUT check)
		if (charTable[raw[i]] & charString) != 0 {
			i++
			start := i

			// SIMD/AVX powered search for the closing quote
			idx := bytes.IndexByte(raw[i:], '"')
			if idx == -1 {
				return ErrTypeMismatch
			}
			i += idx
			keySlice := raw[start:i]
			i++ // Skip closing quote

			idx = bytes.IndexByte(raw[i:], ':')
			if idx == -1 {
				return ErrTypeMismatch
			}
			i += idx + 1 // Jump right after the colon

			// LUT OPTIMIZATION 1: Skip whitespaces
			for i < len(raw) && (charTable[raw[i]]&charSpace) != 0 {
				i++
			}

			// Global null check (SWAR)
			if i+3 < len(raw) {
				if *(*uint32)(unsafe.Pointer(&raw[i])) == nullMagic {
					i += 3
					continue
				}
			}

			hash := intHache.Sum(keySlice)
			if info, ok := reg.Map[hash]; ok {
				switch info.Type {
				case TypeString:
					// LUT check: Verify the value is actually a string
					if (charTable[raw[i]] & charString) == 0 {
						return ErrTypeMismatch
					}
					i++
					strVal, newIdx, err := parseStringIntelligent(raw, i)
					if err != nil {
						return err
					}
					*(*string)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = strVal
					i = newIdx

				case TypeInt:
					valStart := i
					// LUT OPTIMIZATION 2: Find the end of the number.
					// Loop continues until a structural character or whitespace is found.
					for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
						i++
					}
					*(*int)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = fastParseInt(raw[valStart:i])
					i--

				case TypeFloat:
					valStart := i
					// LUT OPTIMIZATION 3: Find the end of the float.
					for i < len(raw) && (charTable[raw[i]]&maskValueEnd) == 0 {
						i++
					}
					*(*float64)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = fastParseFloat(raw[valStart:i])
					i--

				case TypeBool:
					// SWAR optimization for boolean parsing directly into memory
					if i+3 < len(raw) && *(*uint32)(unsafe.Pointer(&raw[i])) == trueMagic {
						*(*bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = true
						i += 3
					} else if raw[i] == 'f' && i+4 < len(raw) && *(*uint32)(unsafe.Pointer(&raw[i+1])) == alseMagic {
						*(*bool)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = false
						i += 4
					} else {
						return ErrTypeMismatch
					}

				case TypeStruct:
					if raw[i] != '{' {
						return ErrTypeMismatch
					}
					end := findBounds(raw, i, '{', '}')
					if end > j {
						subPtr := unsafe.Pointer(uintptr(ptr) + info.Offset)
						_ = ParseObject(raw[i:end+1], info.Sub, subPtr)
					}
					i = end

				case TypeStringSlice:
					if raw[i] != '[' {
						return ErrTypeMismatch
					}
					end := findBounds(raw, i, '[', ']')
					if end > j {
						// Read existing slice to reuse its capacity (Zero-Alloc approach)
						existingSlice := *(*[]string)(unsafe.Pointer(uintptr(ptr) + info.Offset))
						slice, _ := parseStringSliceSafe(raw[i:end+1], existingSlice)
						*(*[]string)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = slice
					}
					i = end

				case TypeIntSlice:
					if raw[i] != '[' {
						return ErrTypeMismatch
					}
					end := findBounds(raw, i, '[', ']')
					if end > j {
						// Read existing slice to reuse its capacity (Zero-Alloc approach)
						existingSlice := *(*[]int)(unsafe.Pointer(uintptr(ptr) + info.Offset))
						*(*[]int)(unsafe.Pointer(uintptr(ptr) + info.Offset)) = parseIntSlice(raw[i:end+1], existingSlice)
					}
					i = end
				}
			} else {
				// If field is not in registry, skip the entire value
				i = skipValue(raw, i)
			}
		}
	}
	return nil
}

func skipValue(raw []byte, i int) int {
	if i >= len(raw) {
		return i
	}
	c := raw[i]

	if c == '"' {
		i++
		for i < len(raw) {
			if raw[i] == '"' {
				return i
			}
			if raw[i] == '\\' {
				i += 2
				continue
			}
			i++
		}
	} else if c == '{' {
		return findBounds(raw, i, '{', '}')
	} else if c == '[' {
		return findBounds(raw, i, '[', ']')
	} else {
		for i < len(raw) && raw[i] != ',' && raw[i] != '}' && raw[i] != ']' && raw[i] != ' ' && raw[i] != '\n' {
			i++
		}
		return i - 1
	}
	return i
}

// UnmarshalSlice processes a raw JSON array sequentially, invoking ParseObject for each element.
func UnmarshalSlice(raw []byte, reg *Registry, slicePtr unsafe.Pointer) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("corrupted json: %v", r)
		}
	}()

	var inObject bool
	var objStart int
	var limit = len(raw)

	for j, k := 0, 1; j < limit; j, k = j+1, k+1 {
		c := raw[j]
		if c == '{' && !inObject {
			inObject = true
			objStart = j
		} else if c == '}' && inObject {
			// Validate end of object boundary
			if j == len(raw)-1 || raw[k] == ',' || raw[k] == '\n' || raw[k] == ' ' || raw[k] == ']' {
				inObject = false

				// Create the object directly within the pre-allocated slice
				err = ParseObject(raw[objStart:k], reg, (unsafe.Pointer)(unsafe.Pointer(uintptr(slicePtr)+uintptr(0))))
				if err != nil {
					return
				}
			}
		}
	}
	return
}

// parseStringIntelligent extracts a string from the JSON buffer.
// It achieves zero-allocation by pointing directly to the raw buffer if no escape characters are present.
func parseStringIntelligent(raw []byte, start int) (string, int, error) {
	hasEscape := false
	i := start

	// Phase 1: Scan for the closing quote and detect escape characters
	for i < len(raw) {
		if raw[i] == '"' {
			// Ensure it's not an escaped quote \"
			if raw[i-1] != '\\' {
				break
			}
		}
		if raw[i] == '\\' {
			hasEscape = true
		}
		i++
	}

	if i >= len(raw) {
		return "", i, fmt.Errorf("unexpected end of JSON input")
	}

	strLen := i - start

	// Phase 2: Fast Path (Zero Allocation)
	// If the string is clean, point directly to the source buffer.
	if !hasEscape {
		if strLen == 0 {
			return "", i + 1, nil
		}
		// Pure zero-copy string instantiation
		return unsafe.String(&raw[start], strLen), i + 1, nil
	}

	// Phase 3: Slow Path (In-Place Allocation-Free Unescaping)
	// We modify the original read-only JSON payload to avoid heap allocations.
	result := unescapeStringInPlace(raw[start:i])

	// Return the result and the NEW INDEX (immediately after the closing quote)
	return result, i + 1, nil
}

func fastParseInt(buf []byte) int {
	res := 0
	for _, b := range buf {
		if b >= '0' && b <= '9' {
			res = res*10 + int(b-'0')
		}
	}
	return res
}

func fastParseFloat(buf []byte) float64 {
	res, fraction, div := 0.0, 0.0, 1.0
	inFrac := false
	for _, b := range buf {
		if b == '.' {
			inFrac = true
			continue
		}
		if b >= '0' && b <= '9' {
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
	dst = dst[:0] // Reset length, retain capacity

	for i := 0; i < len(buf); i++ {
		if buf[i] == '"' {
			i++
			strVal, newIdx, err := parseStringIntelligent(buf, i)
			if err != nil {
				return nil, err
			}
			dst = append(dst, strVal)
			i = newIdx
		}
	}
	return dst, nil
}

// parseIntSlice extracts an array of integers, reusing the provided dst slice capacity.
func parseIntSlice(buf []byte, dst []int) []int {
	if len(buf) <= 2 {
		return nil
	}
	dst = dst[:0] // Reset length, retain capacity

	for i := 0; i < len(buf); i++ {
		if buf[i] >= '0' && buf[i] <= '9' {
			start := i
			for i < len(buf) && buf[i] >= '0' && buf[i] <= '9' {
				i++
			}
			dst = append(dst, fastParseInt(buf[start:i]))
		}
	}
	return dst
}

func findBounds(raw []byte, start int, open, close byte) int {
	depth := 1
	for i := start + 1; i < len(raw); i++ {
		if raw[i] == '"' {
			i++
			for i < len(raw) && raw[i] != '"' {
				if raw[i] == '\\' {
					i++
				}
				i++
			}
			continue
		}
		if raw[i] == open {
			depth++
		} else if raw[i] == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(raw) - 1
}

// ========================== PARALLELISM ==========================

// findObjectBoundaries rapidly locates the boundaries of all top-level objects.
// Returns a slice of slices (zero-copy, referencing the original raw buffer).
func findObjectBoundaries(data []byte) [][]byte {
	var chunks [][]byte
	depth := 0
	inString := false
	start := -1

	for i := 0; i < len(data); i++ {
		c := data[i]

		// String handling to ignore structural characters inside quotes
		if c == '"' && (i == 0 || data[i-1] != '\\') {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		if c == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && start != -1 {
				// Found a complete object boundary, append the slice
				chunks = append(chunks, data[start:i+1])
				start = -1
			}
		}
	}
	return chunks
}

// UnmarshalArrayParallel is the main public generic API for high-performance parallel array parsing.
// It automatically determines the array size, allocates the exact required memory, and distributes parsing across CPU cores.
func UnmarshalArrayParallel[T any](raw []byte, reg *Registry) ([]T, error) {
	// 1. Rapidly scan boundaries. We now know the EXACT number of elements.
	chunks := findObjectBoundaries(raw)
	if len(chunks) == 0 {
		return nil, nil
	}

	// 2. Pre-allocate the strongly-typed slice with the exact required capacity
	result := make([]T, len(chunks))

	// 3. Pass the pointer to the internal worker pool
	err := parseArrayParallelChunks(
		chunks,
		reg,
		unsafe.Pointer(&result[0]),
		unsafe.Sizeof(*new(T)),
	)

	return result, err
}

// ParseArrayParallel parses a JSON array of objects into a pre-allocated memory space.
// Kept for backward compatibility. Prefer UnmarshalArrayParallel[T any].
func ParseArrayParallel(raw []byte, reg *Registry, basePtr unsafe.Pointer, structSize uintptr) error {
	chunks := findObjectBoundaries(raw)
	if len(chunks) == 0 {
		return nil
	}
	return parseArrayParallelChunks(chunks, reg, basePtr, structSize)
}

// parseArrayParallelChunks is the internal worker pool executor.
func parseArrayParallelChunks(chunks [][]byte, reg *Registry, basePtr unsafe.Pointer, structSize uintptr) error {
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > len(chunks) {
		numWorkers = len(chunks)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, numWorkers)
	batchSize := len(chunks) / numWorkers

	// Launch Worker Pool via Batching
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)

		// Calculate index range for the specific worker
		startIdx := w * batchSize
		endIdx := startIdx + batchSize
		if w == numWorkers-1 {
			endIdx = len(chunks) // The last worker processes any remaining items
		}

		go func(start, end int) {
			defer wg.Done()
			for idx := start; idx < end; idx++ {
				chunk := chunks[idx]
				itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(idx) * structSize))

				err := ParseObject(chunk, reg, itemPtr)
				if err != nil {
					errChan <- err
					return
				}
			}
		}(startIdx, endIdx)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors caught by the workers
	if err, ok := <-errChan; ok {
		return err
	}

	return nil
}

// unescapeStringInPlace modifies the provided raw slice in-place, removing escape characters.
// It returns a zero-copy string pointing to the same memory segment.
// WARNING: This mutates the underlying rawJSON byte array.
func unescapeStringInPlace(raw []byte) string {
	// We write to the exact same array from which we read!
	writeIdx := 0
	for readIdx := 0; readIdx < len(raw); readIdx++ {
		if raw[readIdx] == '\\' && readIdx+1 < len(raw) {
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
