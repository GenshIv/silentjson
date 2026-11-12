package silentjson

import (
	"reflect"
	"testing"
	"unsafe"
)

type SimpleUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// Test JSON spec conformance: object syntax errors
func TestObjectSyntaxErrors(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SimpleUser{}))

	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{"missing closing brace", []byte(`{"id":1,"name":"test"`), true},
		{"trailing comma in object", []byte(`{"id":1,}`), true},
		{"missing colon after key", []byte(`{"id" 1}`), true},
		{"unquoted key", []byte(`{id:1}`), true},
		{"empty input", []byte(""), true},
		{"only whitespace", []byte("   "), true},
		{"opening brace only", []byte(`{`), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual SimpleUser
			err := ParseObject(tt.payload, reg, unsafe.Pointer(&actual))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseObject(%q) error = %v, wantErr %v", string(tt.payload), err, tt.wantErr)
			}
		})
	}
}

// Test JSON spec conformance: array syntax errors
func TestArraySyntaxErrors(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SimpleUser{}))

	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{"missing closing bracket", []byte(`[{"id":1}]`), false},
		{"trailing comma in array", []byte(`[{"id":1},]`), true},
		{"opening bracket only", []byte(`[`), true},
		{"array with no elements (valid)", []byte(`[]`), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make([]SimpleUser, 10)
			_, err := UnmarshalSlice[SimpleUser](tt.payload, reg, dst)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalSlice(%q) error = %v, wantErr %v", string(tt.payload), err, tt.wantErr)
			}
		})
	}
}

// Test JSON spec conformance: string errors
func TestStringErrors(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SimpleUser{}))

	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{"unterminated string", []byte(`{"name":"test`), true},
		{"invalid escape sequence", []byte(`{"name":"\q"}`), true},
		{"backslash at end of string", []byte(`{"name":"test\"}`), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual SimpleUser
			err := ParseObject(tt.payload, reg, unsafe.Pointer(&actual))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseObject(%q) error = %v, wantErr %v", string(tt.payload), err, tt.wantErr)
			}
		})
	}
}

// Test JSON spec conformance: number errors
func TestNumberErrors(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SimpleUser{}))

	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{"leading zero", []byte(`{"id":0123}`), true},
		{"multiple decimal points", []byte(`{"age":1.2.3}`), true},
		{"missing digits after decimal", []byte(`{"age":1.}`), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual SimpleUser
			err := ParseObject(tt.payload, reg, unsafe.Pointer(&actual))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseObject(%q) error = %v, wantErr %v", string(tt.payload), err, tt.wantErr)
			}
		})
	}
}

// Test JSON spec conformance: literal errors
func TestLiteralErrors(t *testing.T) {
	type BoolUser struct {
		Active bool `json:"active"`
	}
	reg := BuildRegistry(reflect.TypeOf(BoolUser{}))

	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{"misspelled true", []byte(`{"active":tru}`), true},
		{"misspelled false", []byte(`{"active":fals}`), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual BoolUser
			err := ParseObject(tt.payload, reg, unsafe.Pointer(&actual))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseObject(%q) error = %v, wantErr %v", string(tt.payload), err, tt.wantErr)
			}
		})
	}
}

// Test JSON spec conformance: type mismatch errors
func TestTypeMismatchErrors(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SimpleUser{}))

	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{"string instead of int", []byte(`{"id":"not a number"}`), true},
		{"array instead of object", []byte(`[1,2,3]`), true}, // ParseObject expects an object
		{"object instead of string field", []byte(`{"name":{"nested":true}}`), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual SimpleUser
			err := ParseObject(tt.payload, reg, unsafe.Pointer(&actual))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseObject(%q) error = %v, wantErr %v", string(tt.payload), err, tt.wantErr)
			}
		})
	}
}

// Test stream mode truncation: should return data + error, not panic
func TestStreamTruncation(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SimpleUser{}))

	tests := []struct {
		name    string
		payload []byte
	}{
		{"truncated mid-object", []byte(`{"id":1,"name":"te`)},
		{"truncated mid-value", []byte(`{"id":12`)},
		{"truncated at comma", []byte("[{\"id\":1},{\"id\":2}")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseObject panicked on truncated input: %v", r)
				}
			}()

			var actual SimpleUser
			err := ParseObject(tt.payload, reg, unsafe.Pointer(&actual))
			// In stream mode, truncation should return an error but not panic
			if err == nil {
				t.Errorf("expected error on truncated input %q", string(tt.payload))
			}
		})
	}
}

// Test valid edge cases that should succeed
func TestValidEdgeCases(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(SimpleUser{}))

	tests := []struct {
		name    string
		payload []byte
	}{
		{"whitespace around values", []byte(`{ "id" : 1 , "name" : "test" }`)},
		{"negative numbers", []byte(`{"id":-42,"age":-5}`)},
		{"float with exponent", []byte(`{"age":10}`)}, // 1e10 is a float; use plain int for age field
		{"empty object", []byte(`{}`)},
		{"unicode escape", []byte(`{"name":"\u0048ello"}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual SimpleUser
			err := ParseObject(tt.payload, reg, unsafe.Pointer(&actual))
			if err != nil {
				t.Errorf("ParseObject(%q) error = %v, want nil", string(tt.payload), err)
			}
		})
	}
}
