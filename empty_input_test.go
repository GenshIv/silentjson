package silentjson

import (
	"errors"
	"reflect"
	"testing"
)

type emptyTestRec struct {
	ID int `json:"id"`
}

func TestUnmarshalSliceEmptyInput(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(emptyTestRec{}))

	cases := []struct {
		name    string
		input   []byte
		wantErr error
	}{
		{"empty slice", []byte{}, ErrUnexpectedEOF},
		{"space only", []byte(" "), ErrUnexpectedEOF},
		{"newline and tab only", []byte("\n\t "), ErrUnexpectedEOF},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := make([]emptyTestRec, 4)
			out, err := UnmarshalSlice(c.input, reg, dst)
			if !errors.Is(err, c.wantErr) {
				t.Errorf("got error %v, want %v", err, c.wantErr)
			}
			if len(out) != 0 {
				t.Errorf("got %d elements, want 0", len(out))
			}
		})
	}

	// Valid empty arrays must still succeed
	validEmpty := [][]byte{[]byte("[]"), []byte("[ ]"), []byte("[ \n\t ]")}
	for i, input := range validEmpty {
		t.Run("valid empty "+string(rune('a'+i)), func(t *testing.T) {
			dst := make([]emptyTestRec, 4)
			out, err := UnmarshalSlice(input, reg, dst)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(out) != 0 {
				t.Errorf("got %d elements, want 0", len(out))
			}
		})
	}

	// Pre-populated destination must be returned with zero length
	t.Run("pre-populated dst on empty input", func(t *testing.T) {
		dst := []emptyTestRec{{ID: 1}, {ID: 2}}
		out, err := UnmarshalSlice([]byte{}, reg, dst)
		if !errors.Is(err, ErrUnexpectedEOF) {
			t.Errorf("got error %v, want ErrUnexpectedEOF", err)
		}
		if len(out) != 0 {
			t.Errorf("got %d elements, want 0", len(out))
		}
	})
}

func TestUnmarshalSliceTolerantEmptyInput(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(emptyTestRec{}))

	cases := []struct {
		name      string
		input     []byte
		wantProbs int
	}{
		{"empty slice", []byte{}, 1},
		{"space only", []byte(" "), 1},
		{"newline and tab only", []byte("\n\t "), 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := make([]emptyTestRec, 4)
			out, problems := UnmarshalSliceTolerant(c.input, reg, dst)
			if len(problems) != c.wantProbs {
				t.Errorf("got %d problems, want %d", len(problems), c.wantProbs)
			} else {
				p := problems[0]
				if p.Offset != 0 {
					t.Errorf("problem offset %d, want 0", p.Offset)
				}
				if p.Record != -1 {
					t.Errorf("problem record %d, want -1", p.Record)
				}
				if !errors.Is(p.Err, ErrUnexpectedEOF) {
					t.Errorf("problem error %v does not wrap ErrUnexpectedEOF", p.Err)
				}
			}
			if len(out) != 0 {
				t.Errorf("got %d elements, want 0", len(out))
			}
		})
	}

	// Valid empty arrays must produce no problems
	validEmpty := [][]byte{[]byte("[]"), []byte("[ ]"), []byte("[ \n\t ]")}
	for i, input := range validEmpty {
		t.Run("valid empty "+string(rune('a'+i)), func(t *testing.T) {
			dst := make([]emptyTestRec, 4)
			out, problems := UnmarshalSliceTolerant(input, reg, dst)
			if len(problems) != 0 {
				t.Errorf("got %d problems, want 0: %v", len(problems), problems)
			}
			if len(out) != 0 {
				t.Errorf("got %d elements, want 0", len(out))
			}
		})
	}

	// Pre-populated destination on empty input
	t.Run("pre-populated dst on empty input", func(t *testing.T) {
		dst := []emptyTestRec{{ID: 1}, {ID: 2}}
		out, problems := UnmarshalSliceTolerant([]byte{}, reg, dst)
		if len(problems) != 1 {
			t.Errorf("got %d problems, want 1", len(problems))
		}
		if !errors.Is(problems[0].Err, ErrUnexpectedEOF) {
			t.Errorf("problem error %v does not wrap ErrUnexpectedEOF", problems[0].Err)
		}
		if len(out) != 0 {
			t.Errorf("got %d elements, want 0", len(out))
		}
	})
}

func TestUnmarshalArrayParallelEmptyInput(t *testing.T) {
	reg := BuildRegistry(reflect.TypeOf(emptyTestRec{}))

	cases := []struct {
		name    string
		input   []byte
		wantErr error
	}{
		{"empty slice", []byte{}, ErrUnexpectedEOF},
		{"space only", []byte(" "), ErrUnexpectedEOF},
		{"newline and tab only", []byte("\n\t "), ErrUnexpectedEOF},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := make([]emptyTestRec, 4)
			out, err := UnmarshalArrayParallel(c.input, reg, dst)
			if !errors.Is(err, c.wantErr) {
				t.Errorf("got error %v, want %v", err, c.wantErr)
			}
			if out != nil && len(out) != 0 {
				t.Errorf("got %d elements, want 0", len(out))
			}
		})
	}

	// Valid empty arrays must still succeed
	validEmpty := [][]byte{[]byte("[]"), []byte("[ ]"), []byte("[ \n\t ]")}
	for i, input := range validEmpty {
		t.Run("valid empty "+string(rune('a'+i)), func(t *testing.T) {
			dst := make([]emptyTestRec, 4)
			out, err := UnmarshalArrayParallel(input, reg, dst)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(out) != 0 {
				t.Errorf("got %d elements, want 0", len(out))
			}
		})
	}
}
