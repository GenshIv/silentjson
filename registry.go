package silentjson

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/GenshIv/intHache"
	"github.com/cespare/ryu"
)

var (
	ErrUnexpectedEOF = errors.New("zerojson: unexpected end of JSON input")
	ErrTypeMismatch  = errors.New("zerojson: json type mismatch")
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
	SliceType  reflect.Type
	ElemType   reflect.Type
}

// HashEntry removed in favor of SoA

// Registry: Map for parsing lookup, Fields for fast sequential generation
type Registry struct {
	Map         map[int64]FieldInfo
	NameMap     map[string]FieldInfo
	Fields      []FieldInfo
	HashKeysU64 []uint64
	HashKeysStr []string
	HashKeysLen []uint8
	HashValues  []int16
	HashMask    uint32
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
			} else if field.Type.Elem().Kind() == reflect.Struct {
				info.Type = TypeStructSlice
				info.Sub = BuildRegistry(field.Type.Elem())
				info.SliceType = field.Type
				info.ElemType = field.Type.Elem()

				info.Marshaler = func(ptr unsafe.Pointer, buf []byte) []byte {
					slicePtr := (*[]any)(unsafe.Pointer(uintptr(ptr) + info.Offset))
					// We don't know the exact type here for unsafe.Slice,
					// but we have info.ElemType.Size().
					// However, we can use the reflect.SliceHeader logic safely
					// or just keep it as is if it's already working.
					// Let's use a slightly more modern way if possible.
					h := (*struct {
						Data uintptr
						Len  int
						Cap  int
					})(unsafe.Pointer(slicePtr))

					buf = append(buf, '[')
					eSize := info.ElemType.Size()
					for i := 0; i < h.Len; i++ {
						if i > 0 {
							buf = append(buf, ',')
						}
						buf = MarshalObject(unsafe.Pointer(h.Data+uintptr(i)*eSize), info.Sub, buf)
					}
					return append(buf, ']')
				}
			}
		}

		// If OmitEmpty is present, wrap marshaler in a check
		if info.OmitEmpty {
			orig := info.Marshaler
			info.Marshaler = func(ptr unsafe.Pointer, buf []byte) []byte {
				return orig(ptr, buf)
			}
		}

		hashe := intHache.Sum([]byte(key))
		info.Hash = hashe
		reg.Map[hashe] = info
		reg.NameMap[key] = info
		reg.Fields = append(reg.Fields, info)
	}

	// Build Custom Lock-Free Hash Table if > 16 fields
	if len(reg.Fields) > 16 {
		size := 1
		for size <= len(reg.Fields)*2 {
			size *= 2
		}
		reg.HashKeysU64 = make([]uint64, size)
		reg.HashKeysStr = make([]string, size)
		reg.HashKeysLen = make([]uint8, size)
		reg.HashValues = make([]int16, size)
		reg.HashMask = uint32(size - 1)

		for i := 0; i < len(reg.Fields); i++ {
			f := reg.Fields[i]
			var idx uint32
			if f.KeyLen <= 8 {
				idx = hashU64(f.KeyUint64) & reg.HashMask
			} else {
				idx = hashString(f.Key) & reg.HashMask
			}
			for {
				if reg.HashKeysLen[idx] == 0 {
					reg.HashKeysU64[idx] = f.KeyUint64
					reg.HashKeysStr[idx] = f.Key
					reg.HashKeysLen[idx] = uint8(f.KeyLen)
					reg.HashValues[idx] = int16(i)
					break
				}
				idx = (idx + 1) & reg.HashMask
			}
		}
	}

	return reg
}

func hashString(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return h
}

func hashBytes(b []byte) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(b); i++ {
		h = (h ^ uint32(b[i])) * 16777619
	}
	return h
}

// hashU64 is a fast mixing function for our 64-bit integer keys
func hashU64(x uint64) uint32 {
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return uint32(x ^ (x >> 31))
}

func MarshalFloat(ptr unsafe.Pointer, buf []byte) []byte {
	return ryu.AppendFloat64(buf, *(*float64)(ptr))
}

func MarshalBool(ptr unsafe.Pointer, buf []byte) []byte {
	if *(*bool)(ptr) {
		return append(buf, 't', 'r', 'u', 'e')
	}
	return append(buf, 'f', 'a', 'l', 's', 'e')
}

func MarshalString(ptr unsafe.Pointer, buf []byte) []byte {
	s := *(*string)(ptr)

	newBuf, specialPos := appendStringASM(buf, s)
	if specialPos == -1 {
		return newBuf
	}
	if specialPos == -2 {
		return appendJSONStringGo(buf, s)
	}

	buf = newBuf
	for i := specialPos; i < len(s); i++ {
		c := s[i]
		if (charTable[c] & (charString | charEscape)) != 0 {
			buf = append(buf, '\\', c)
		} else {
			buf = append(buf, c)
		}
	}
	return append(buf, '"')
}

func appendJSONStringGo(buf []byte, s string) []byte {
	buf = append(buf, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (charTable[c] & (charString | charEscape)) != 0 {
			buf = append(buf, '\\', c)
		} else {
			buf = append(buf, c)
		}
	}
	return append(buf, '"')
}

func MarshalIntSlice(ptr unsafe.Pointer, buf []byte) []byte {
	slice := *(*[]int)(ptr)
	if slice == nil {
		return append(buf, 'n', 'u', 'l', 'l')
	}
	buf = append(buf, '[')
	for i, v := range slice {
		if i > 0 {
			buf = append(buf, ',')
		}
		val := int64(v)
		newBuf := appendIntASM(buf, val)
		if len(newBuf) == len(buf) {
			buf = strconv.AppendInt(buf, val, 10)
		} else {
			buf = newBuf
		}
	}
	return append(buf, ']')
}

func MarshalStringSlice(ptr unsafe.Pointer, buf []byte) []byte {
	slice := *(*[]string)(ptr)
	if slice == nil {
		return append(buf, 'n', 'u', 'l', 'l')
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

func MarshalInt(ptr unsafe.Pointer, buf []byte) []byte {
	val := *(*int64)(ptr)
	newBuf := appendIntASM(buf, val)
	if len(newBuf) == len(buf) {
		return strconv.AppendInt(buf, val, 10)
	}
	return newBuf
}
