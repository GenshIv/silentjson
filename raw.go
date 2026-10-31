package silentjson

// RawMessage is a raw encoded JSON value, similar to json.RawMessage.
// It is serialized as-is by silentjson.Marshal without any escaping or modification.
type RawMessage []byte

// GetStringValue returns the string value for a given key in a JSON object.
// Top-level keys are searched first (like encoding/json); if the key is not
// found at the top level, nested objects and arrays are searched depth-first.
// The input is never modified; keys are matched after unescaping, so string
// VALUES can never produce false matches. O(n) scan, no alloc except the
// returned string.
func GetStringValue(data []byte, key string) (string, bool) {
	val, ok := findValueTop(data, key)
	if !ok {
		val, ok = findValueDeep(data, key, 0)
	}
	if !ok || len(val) == 0 || val[0] != '"' {
		return "", false
	}
	s, _, _, err := readJSONString(val[1:], false)
	if err != nil {
		return "", false
	}
	return s, true
}

// GetInt64Value returns the int64 value for a given key in a JSON object.
// Top-level keys are searched first; nested objects and arrays are searched
// depth-first as a fallback. Only strict JSON integers match ("1e2" or "3.9"
// return ok=false). O(n) scan, no alloc.
func GetInt64Value(data []byte, key string) (int64, bool) {
	val, ok := findValueTop(data, key)
	if !ok {
		val, ok = findValueDeep(data, key, 0)
	}
	if !ok {
		return 0, false
	}
	return parseJSONInt(val)
}

const rawSearchDepthLimit = 32

// findValueTop returns the raw value bytes of key at the TOP level of the
// JSON object in data. It never descends into nested structures.
func findValueTop(data []byte, key string) ([]byte, bool) {
	i := 0
	for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
		i++
	}
	if i >= len(data) || data[i] != '{' {
		return nil, false
	}
	i++
	for {
		for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(data) {
			return nil, false
		}
		if data[i] == '}' {
			return nil, false
		}
		if data[i] != '"' {
			return nil, false
		}
		i++
		k, _, consumed, err := readJSONString(data[i:], false)
		if err != nil {
			return nil, false
		}
		i += int(consumed)
		for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(data) || data[i] != ':' {
			return nil, false
		}
		i++
		for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(data) {
			return nil, false
		}
		end := skipValue(data, i)
		if end > len(data) {
			end = len(data)
		}
		if k == key {
			return data[i:end], true
		}
		i = end
		for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
			i++
		}
		if i >= len(data) {
			return nil, false
		}
		if data[i] == ',' {
			i++
			continue
		}
		return nil, false
	}
}

// findValueDeep searches nested objects and arrays of data for key,
// depth-first. Only called when the top level does not have the key.
func findValueDeep(data []byte, key string, depth int) ([]byte, bool) {
	if depth > rawSearchDepthLimit {
		return nil, false
	}
	i := 0
	for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
		i++
	}
	if i >= len(data) {
		return nil, false
	}
	switch data[i] {
	case '{':
		i++
		for {
			for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
				i++
			}
			if i >= len(data) || data[i] == '}' || data[i] != '"' {
				return nil, false
			}
			i++
			k, _, consumed, err := readJSONString(data[i:], false)
			if err != nil {
				return nil, false
			}
			i += int(consumed)
			for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
				i++
			}
			if i >= len(data) || data[i] != ':' {
				return nil, false
			}
			i++
			for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
				i++
			}
			if i >= len(data) {
				return nil, false
			}
			end := skipValue(data, i)
			if end > len(data) {
				end = len(data)
			}
			if k != key && (data[i] == '{' || data[i] == '[') {
				if v, ok := findValueDeep(data[i:end], key, depth+1); ok {
					return v, true
				}
			}
			if k == key {
				return data[i:end], true
			}
			i = end
			for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
				i++
			}
			if i >= len(data) {
				return nil, false
			}
			if data[i] == ',' {
				i++
				continue
			}
			return nil, false
		}
	case '[':
		i++
		for {
			for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
				i++
			}
			if i >= len(data) || data[i] == ']' {
				return nil, false
			}
			end := skipValue(data, i)
			if end > len(data) {
				end = len(data)
			}
			if data[i] == '{' || data[i] == '[' {
				if v, ok := findValueDeep(data[i:end], key, depth+1); ok {
					return v, true
				}
			}
			i = end
			for i < len(data) && (charTable[data[i]]&charSpace) != 0 {
				i++
			}
			if i >= len(data) {
				return nil, false
			}
			if data[i] == ',' {
				i++
				continue
			}
			return nil, false
		}
	}
	return nil, false
}

// InjectFieldBeforeClose инжектит поле перед закрывающей фигурной скобкой.
// Вставляет [,"]<key>":"<value>" перед последней '}'. value должен быть
// уже экранирован. Вход обязан быть JSON-объектом: иначе data возвращается
// без изменений (никаких паник на пустом/битом входе). Для пустого объекта
// "{}" запятая не добавляется. Аллокация — только итоговый буфер.
func InjectFieldBeforeClose(data []byte, key, value []byte) []byte {
	if len(data) == 0 || data[len(data)-1] != '}' {
		return data
	}
	start := 0
	for start < len(data) && (charTable[data[start]]&charSpace) != 0 {
		start++
	}
	if start >= len(data) || data[start] != '{' {
		return data
	}

	body := data[:len(data)-1]
	j := len(body) - 1
	for j >= 0 && (charTable[body[j]]&charSpace) != 0 {
		j--
	}
	needComma := j >= 0 && body[j] != '{'

	result := make([]byte, 0, len(body)+len(key)+len(value)+8)
	result = append(result, body...)
	if needComma {
		result = append(result, ',')
	}
	result = append(result, '"')
	result = appendJSONKey(result, key)
	result = append(result, '"', ':', '"')
	result = append(result, value...)
	result = append(result, '"', '}')
	return result
}

// appendJSONKey appends the key bytes, escaping quotes/backslashes/control
// characters only when needed (no string conversion in the fast path).
func appendJSONKey(buf []byte, key []byte) []byte {
	for _, c := range key {
		if c == '"' || c == '\\' || c < 0x20 {
			return appendJSONStringGo(buf, string(key))
		}
	}
	return append(buf, key...)
}

// InjectFieldBeforeClose inserts a field before the closing '}' of a JSON object.
// Inserts [,"]<key>":"<value>" right before the final '}'. value must be
// already escaped. The input must be a JSON object; otherwise data is
// returned unchanged. No parse, no alloc except the result buffer.
func InjectFieldBeforeClose1(data []byte, key, value []byte) []byte {
	if len(data) == 0 || data[len(data)-1] != '}' {
		return data
	}
	start := 0
	for start < len(data) && (charTable[data[start]]&charSpace) != 0 {
		start++
	}
	if start >= len(data) || data[start] != '{' {
		return data
	}

	body := data[:len(data)-1]
	j := len(body) - 1
	for j >= 0 && (charTable[body[j]]&charSpace) != 0 {
		j--
	}
	needComma := j >= 0 && body[j] != '{'

	result := make([]byte, 0, len(body)+len(key)+len(value)+8)
	result = append(result, body...)
	if needComma {
		result = append(result, ',')
	}
	result = append(result, '"')
	result = appendJSONKey(result, key)
	result = append(result, '"', ':', '"')
	result = append(result, value...)
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
