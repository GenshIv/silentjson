package silentjson

// Tolerant parsing: process what can be processed, report the rest.
//
// The library's philosophy for real-world (streaming, HFT, partially
// trusted feeds) data is that a damaged record must not discard the
// healthy ones: the parser isolates every problem it detects, keeps the
// successfully decoded records and describes what it had to skip.
// What to DO with the problems is the caller's decision — treat them as
// fatal (the strict APIs are exactly that policy), log them, or ignore.
//
// The strictness work (grammar validation, separator checks, truncation
// detection) is the detection mechanism feeding this reporting layer:
// the same checks run either way, only the policy differs.
//
// Problem offsets are best-effort positions within the scanned region
// (for streams, within the decoder's current buffer window).

import "fmt"

// Problem describes one recoverable defect the parser isolated instead
// of failing the whole input.
type Problem struct {
	// Offset is the best-effort byte position of the defect within the
	// scanned region (for streams: the decoder's current buffer window).
	Offset int
	// Record is the element index the problem relates to, or -1 when the
	// problem is structural (separator, unterminated array, trailing data).
	Record int
	// Err describes the defect.
	Err error
}

func (p Problem) String() string {
	if p.Record >= 0 {
		return fmt.Sprintf("record %d at offset %d: %v", p.Record, p.Offset, p.Err)
	}
	return fmt.Sprintf("offset %d: %v", p.Offset, p.Err)
}

// problemList is grown lazily: parsing clean input allocates nothing.
type problemList struct {
	items []Problem
}

func (l *problemList) add(offset, record int, err error) {
	l.items = append(l.items, Problem{Offset: offset, Record: record, Err: err})
}

func (l *problemList) orNil() []Problem {
	if len(l.items) == 0 {
		return nil
	}
	return l.items
}

// firstErr converts the collected problems into a fail-fast error —
// this is the entire difference between the tolerant engine and the
// strict APIs layered on top of it.
func firstErr(problems []Problem) error {
	if len(problems) == 0 {
		return nil
	}
	return problems[0].Err
}
