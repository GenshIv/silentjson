package silentjson

import (
	"strconv"
	"unsafe"
)

// RawMessage is a raw encoded JSON value, similar to json.RawMessage.
// It is serialized as-is by silentjson.Marshal without any escaping or modification.
type RawMessage []byte

// GetStringValue returns the string value for a given key in a JSON object.
// Format expected: "key":"value" or "key": "value"
// Returns (value, ok). O(n) scan, no alloc except the returned string.
func GetStringValue(data []byte, key string) (string, bool) {
	if len(data) == 0 {
		return "", false
	}

	// Search for "key"
	pattern := []byte(`"` + key + `":`)
	idx := findBytes(data, pattern)
	if idx < 0 {
		return "", false
	}

	// Skip past the colon
	valStart := idx + len(pattern)

	// Skip whitespace
	for valStart < len(data) && (charTable[data[valStart]]&charSpace) != 0 {
		valStart++
	}

	if valStart >= len(data) || data[valStart] != '"' {
		return "", false
	}

	// Parse JSON string
	written, consumed := parseShortStringASM2(data[valStart:])
	if consumed < 0 {
		return "", false
	}

	decoded := data[valStart : valStart+int(written)]
	return unsafe.String(unsafe.SliceData(decoded), len(decoded)), true
}

// GetInt64Value returns the int64 value for a given key in a JSON object.
// Format expected: "key":123 or "key": 123
// Returns (value, ok). O(n) scan, no alloc.
func GetInt64Value(data []byte, key string) (int64, bool) {
	if len(data) == 0 {
		return 0, false
	}

	// Search for "key"
	pattern := []byte(`"` + key + `":`)
	idx := findBytes(data, pattern)
	if idx < 0 {
		return 0, false
	}

	// Skip past the colon
	valStart := idx + len(pattern)

	// Skip whitespace
	for valStart < len(data) && (charTable[data[valStart]]&charSpace) != 0 {
		valStart++
	}

	if valStart >= len(data) {
		return 0, false
	}

	// Read number
	valEnd := valStart
	for valEnd < len(data) && (charTable[data[valEnd]]&maskValueEnd) == 0 {
		valEnd++
	}

	numStr := data[valStart:valEnd]
	if len(numStr) == 0 {
		return 0, false
	}

	val, err := strconv.ParseInt(unsafe.String(unsafe.SliceData(numStr), len(numStr)), 10, 64)
	if err != nil {
		return 0, false
	}

	return val, true
}

// InjectFieldBeforeClose inserts a field before the closing '}' of a JSON object.
// Inserts ,"<key>":"<value>" right before the final '}'.
// No parse, no alloc except the result buffer.
func InjectFieldBeforeClose(data []byte, key, value string) []byte {
	if len(data) == 0 || data[len(data)-1] != '}' {
		return data
	}

	// Escape value for JSON string
	escaped := escapeJSONString(value)

	// Single allocation: [data[:len-1] + ","key":"escaped"" + }
	insertLen := 4 + len(key) + 3 + len(escaped)
	result := make([]byte, 0, len(data)+insertLen)
	result = append(result, data[:len(data)-1]...)
	result = append(result, ',', '"')
	result = append(result, key...)
	result = append(result, '"', ':', '"')
	result = append(result, escaped...)
	result = append(result, '"', '}')
	return result
}

// escapeJSONString escapes a string for use in a JSON string literal.
// Handles ", \, and control characters (including \uXXXX for non-ASCII if needed).
func escapeJSONString(s string) []byte {
	if s == "" {
		return nil
	}

	// Fast path: no escaping needed
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 {
			goto slow
		}
	}
	return []byte(s)

slow:
	buf := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			buf = append(buf, '\\', '"')
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		case '\t':
			buf = append(buf, '\\', 't')
		case '\b':
			buf = append(buf, '\\', 'b')
		case '\f':
			buf = append(buf, '\\', 'f')
		default:
			if c < 0x20 {
				// Control character: \u00XX
				buf = append(buf, '\\', 'u', '0', '0',
					hex[c>>4], hex[c&0x0f])
			} else {
				buf = append(buf, c)
			}
		}
	}
	return buf
}

var hex = "0123456789abcdef"

// EscapeStringInto writes a JSON-escaped string into w, without quotes.
// No allocations except for control chars via small buffer.
func EscapeStringInto(w interface{ Write([]byte) (int, error) }, s string) {
	if s == "" {
		return
	}

	// Fast path: no escaping needed
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 {
			goto slow
		}
	}
	_, _ = w.Write([]byte(s))
	return

slow:
	buf := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			buf = append(buf, '\\', '"')
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		case '\t':
			buf = append(buf, '\\', 't')
		case '\b':
			buf = append(buf, '\\', 'b')
		case '\f':
			buf = append(buf, '\\', 'f')
		default:
			if c < 0x20 {
				buf = append(buf, '\\', 'u', '0', '0',
					hex[c>>4], hex[c&0x0f])
			} else {
				buf = append(buf, c)
			}
		}
	}
	_, _ = w.Write(buf)
}

// findBytes finds the first occurrence of pattern in data.
// Simple O(n*m) scan, sufficient for short patterns like "\"key\":".
func findBytes(data, pattern []byte) int {
	if len(pattern) == 0 || len(data) < len(pattern) {
		return -1
	}

	for i := 0; i <= len(data)-len(pattern); i++ {
		match := true
		for j := 0; j < len(pattern); j++ {
			if data[i+j] != pattern[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
