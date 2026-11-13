package silentjson

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"unsafe"
)

// validateRegistryType checks that the registry was built for type T.
// Uses exact reflect.Type identity, not size comparison.
func validateRegistryType[T any](reg *Registry) error {
	if reg == nil {
		return errors.New("registry is nil")
	}
	if reg.RootType == nil {
		return errors.New("registry RootType is nil (BuildRegistry was not called with the correct type)")
	}
	dstType := reflect.TypeOf((*T)(nil)).Elem()
	if dstType != reg.RootType {
		return fmt.Errorf("registry built for %s, but destination type is %s", reg.RootType.String(), dstType.String())
	}
	return nil
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

// emptyArrayRemnant reports whether the remaining scan window is only
// whitespace, a closing bracket and more whitespace — i.e. a legitimately
// EMPTY array. A zero chunk count with any other remnant means the
// scanner desynchronized on malformed input (e.g. an unterminated
// string) and must be rejected like encoding/json rejects it, not
// silently reported as "zero records".
// arrayTailClosed reports whether inner[pos:] is only whitespace, the
// closing bracket and more whitespace: the array is properly terminated
// with nothing trailing. The slice APIs require this — encoding/json
// rejects a truncated array and so must they.
func arrayTailClosed(inner []byte, pos int) bool {
	i := pos
	for i < len(inner) && (charTable[inner[i]]&charSpace) != 0 {
		i++
	}
	if i >= len(inner) || inner[i] != ']' {
		return false
	}
	i++
	for i < len(inner) && (charTable[inner[i]]&charSpace) != 0 {
		i++
	}
	return i == len(inner)
}

// validArrayGap reports whether gap is a legal separator between array
// elements: only whitespace before the first element, and exactly one
// comma surrounded by whitespace between the others. Leading, doubled
// or stray commas must be rejected like encoding/json does — the
// splitter emits phantom empty chunks on commas and callers filter them
// silently, so the separators themselves need this check.
func validArrayGap(gap []byte, expectComma bool) bool {
	i := 0
	for i < len(gap) && (charTable[gap[i]]&charSpace) != 0 {
		i++
	}
	if expectComma {
		if i >= len(gap) || gap[i] != ',' {
			return false
		}
		i++
		for i < len(gap) && (charTable[gap[i]]&charSpace) != 0 {
			i++
		}
	}
	return i == len(gap)
}

func emptyArrayRemnant(inner []byte) bool {
	i := 0
	for i < len(inner) && (charTable[inner[i]]&charSpace) != 0 {
		i++
	}
	if i >= len(inner) || inner[i] != ']' {
		return false
	}
	i++
	for i < len(inner) && (charTable[inner[i]]&charSpace) != 0 {
		i++
	}
	return i == len(inner)
}

func findObjectBoundaries(data []byte, buf []Chunk) ([]Chunk, int) {
	count, maxSize := findObjectBoundariesASM(data, buf)
	if count > len(buf) {
		count = len(buf)
	}
	return buf[:count], maxSize
}

// UnmarshalSlice processes a raw JSON array sequentially, invoking ParseObject for each element.
func UnmarshalSlice[T any](raw []byte, reg *Registry, dst []T) ([]T, error) {
	if len(raw) == 0 {
		return dst[:0], nil
	}

	// Validate that the registry was built for type T using exact type identity.
	if err := validateRegistryType[T](reg); err != nil {
		return nil, fmt.Errorf("UnmarshalSlice: %w", err)
	}

	buf := reg.chunkPool.Get().([]Chunk)
	need := estimateChunkCapacity(raw)
	if cap(buf) < need {
		buf = make([]Chunk, need)
	}

	startIdx := skipSpaceASM(raw, 0)
	if startIdx < 0 || startIdx >= len(raw) || raw[startIdx] != '[' {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, errors.New("expected '[' at the beginning of array")
	}
	rawInner := raw[startIdx+1:]

	// The splitter stops when the chunk buffer fills (its documented
	// "early exit" contract), so arrays with more elements than the
	// buffer estimate are scanned in repeated windows until the whole
	// array is consumed. A single scan silently returned only the first
	// window's records — e.g. 500k tiny records against a ~157k-chunk
	// estimate parsed just 31% of the payload — until the array-tail
	// check turned the silent truncation into a loud error.
	scanOffset := 0
	total := 0
	prevEndAbs := 0 // absolute end of the last accepted element
	structSize := unsafe.Sizeof(*new(T))

	// Defer basePtr computation until we know there's at least one element
	// to write; taking &dst[0] on an empty slice panics.
	var basePtr unsafe.Pointer
	if len(dst) > 0 {
		basePtr = unsafe.Pointer(&dst[0])
	}

	var err error
	for scanOffset < len(rawInner) {
		count, depth := findArrayElementsEarlyExitASM(rawInner[scanOffset:], buf[:len(buf)])
		if count < 0 || count > len(buf) {
			reg.chunkPool.Put(buf[:cap(buf)])
			return nil, fmt.Errorf("asm returned invalid count: %d", count)
		}
		if count == 0 {
			if !emptyArrayRemnant(rawInner[scanOffset:]) {
				reg.chunkPool.Put(buf[:cap(buf)])
				return nil, fmt.Errorf("%w: malformed array element (unbalanced braces or unterminated string, scanner depth %d)", ErrTypeMismatch, depth)
			}
			break
		}
		stepAdvance := buf[count-1].End // capture before compaction below

		validCount := 0
		prevEnd := 0 // window-relative
		for i := 0; i < count; i++ {
			if buf[i].Start == buf[i].End {
				continue // phantom chunk emitted on a stray comma
			}
			// Separator grammar folded into the phantom filter. For the
			// first element of a resumed window the gap starts right at
			// the separating comma left at the window start.
			gapStart := prevEnd
			if validCount == 0 {
				gapStart = 0
			}
			if !validArrayGap(rawInner[scanOffset+gapStart:scanOffset+buf[i].Start], total > 0 || validCount > 0) {
				reg.chunkPool.Put(buf[:cap(buf)])
				return nil, fmt.Errorf("%w: expected ',' between array elements at %d", ErrTypeMismatch, scanOffset+buf[i].Start)
			}
			buf[validCount] = buf[i]
			validCount++
			prevEnd = buf[i].End
		}
		if validCount == 0 {
			// window held only phantom chunks (e.g. the lone closing
			// bracket): legitimate only as an empty-array remnant
			if !emptyArrayRemnant(rawInner[scanOffset:]) {
				reg.chunkPool.Put(buf[:cap(buf)])
				return nil, fmt.Errorf("%w: malformed array element (unbalanced braces or unterminated string, scanner depth %d)", ErrTypeMismatch, depth)
			}
			break
		}
		if total+validCount > len(dst) {
			reg.chunkPool.Put(buf[:cap(buf)])
			return nil, fmt.Errorf("insufficient capacity: need %d, have %d", total+validCount, len(dst))
		}

		for j := 0; j < validCount; j++ {
			chunk := buf[j]
			absS, absE := scanOffset+chunk.Start, scanOffset+chunk.End
			if absS < 0 || absE > len(rawInner) || absS >= absE {
				err = fmt.Errorf("invalid json boundaries at chunk %d", total+j)
				break
			}
			itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(total+j) * structSize))
			if err = ParseObject(rawInner[absS:absE], reg, itemPtr); err != nil {
				break
			}
		}
		if err != nil {
			break
		}
		total += validCount
		prevEndAbs = scanOffset + prevEnd
		next := scanOffset + stepAdvance
		if next <= scanOffset {
			reg.chunkPool.Put(buf[:cap(buf)])
			return nil, fmt.Errorf("%w: scanner did not advance", ErrTypeMismatch)
		}
		scanOffset = next
	}

	reg.chunkPool.Put(buf[:cap(buf)])
	if err != nil {
		return nil, err
	}
	// No early return for total == 0: the tail epilogue must run on EVERY
	// path, or bytes that belong to neither a record nor a validated
	// separator fall through silently. With no scanned windows
	// prevEndAbs == 0, so the check covers the whole inner region and
	// rejects a bare "[" (previously accepted as an empty array, while
	// "[ " was rejected - an accident of the loop-entry condition).
	// "[]"/"[ ]" still succeed: their remnant is whitespace + ']' + ws.
	if !arrayTailClosed(rawInner, prevEndAbs) {
		return nil, fmt.Errorf("%w: unterminated array (missing closing bracket or trailing data)", ErrTypeMismatch)
	}
	return dst[:total], nil
}

func UnmarshalSliceTolerant[T any](raw []byte, reg *Registry, dst []T) ([]T, []Problem) {
	if len(raw) == 0 {
		return dst[:0], nil
	}

	// Validate that the registry was built for type T using exact type identity.
	if err := validateRegistryType[T](reg); err != nil {
		return nil, []Problem{{Offset: 0, Record: -1, Err: fmt.Errorf("UnmarshalSliceTolerant: %w", err)}}
	}

	buf := reg.chunkPool.Get().([]Chunk)
	need := estimateChunkCapacity(raw)
	if cap(buf) < need {
		buf = make([]Chunk, need)
	}

	startIdx := skipSpaceASM(raw, 0)
	if startIdx < 0 || startIdx >= len(raw) || raw[startIdx] != '[' {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, []Problem{{Offset: 0, Record: -1, Err: errors.New("expected '[' at the beginning of array")}}
	}
	rawInner := raw[startIdx+1:]

	var problems problemList
	scanOffset := 0
	written := 0
	prevEndAbs := 0
	structSize := unsafe.Sizeof(*new(T))

	// Defer basePtr computation until we know there's at least one element
	// to write; taking &dst[0] on an empty slice panics.
	var basePtr unsafe.Pointer
	if len(dst) > 0 {
		basePtr = unsafe.Pointer(&dst[0])
	}

	for scanOffset < len(rawInner) {
		count, _ := findArrayElementsEarlyExitASM(rawInner[scanOffset:], buf[:len(buf)])
		if count < 0 || count > len(buf) {
			reg.chunkPool.Put(buf[:cap(buf)])
			return nil, []Problem{{Offset: scanOffset, Record: -1, Err: fmt.Errorf("asm returned invalid count: %d", count)}}
		}
		if count == 0 {
			// No complete element in this window: either the array ended
			// (legitimate empty remnant) or the tail is damaged/truncated.
			// Do not report here — the tail stage below salvages a valid
			// last element and reports the unterminated array exactly
			// once, instead of double-reporting the same defect.
			break
		}
		stepAdvance := buf[count-1].End // capture before compaction below

		validCount := 0
		prevEnd := 0
		for i := 0; i < count; i++ {
			if buf[i].Start == buf[i].End {
				continue
			}
			gapStart := prevEnd
			if validCount == 0 {
				gapStart = 0
			}
			if !validArrayGap(rawInner[scanOffset+gapStart:scanOffset+buf[i].Start], written > 0 || validCount > 0) {
				problems.add(scanOffset+buf[i].Start, -1, fmt.Errorf("%w: bad separator between elements", ErrTypeMismatch))
			}
			buf[validCount] = buf[i]
			validCount++
			prevEnd = buf[i].End
		}
		if validCount == 0 {
			break // phantom-only window (e.g. the lone ']'): nothing to do
		}
		if written+validCount > len(dst) {
			reg.chunkPool.Put(buf[:cap(buf)])
			return nil, append(problems.orNil(), Problem{Offset: scanOffset, Record: -1,
				Err: fmt.Errorf("insufficient capacity: need %d, have %d", written+validCount, len(dst))})
		}

		for j := 0; j < validCount; j++ {
			chunk := buf[j]
			absS, absE := scanOffset+chunk.Start, scanOffset+chunk.End
			if absS < 0 || absE > len(rawInner) || absS >= absE {
				problems.add(absS, written+j, fmt.Errorf("%w: invalid element boundaries", ErrTypeMismatch))
				continue
			}
			itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(written) * structSize))
			if perr := ParseObject(rawInner[absS:absE], reg, itemPtr); perr != nil {
				problems.add(absS, written+j, perr)
				continue // skip the damaged record, keep parsing
			}
			written++
		}
		prevEndAbs = scanOffset + prevEnd
		next := scanOffset + stepAdvance
		if next <= scanOffset {
			break
		}
		scanOffset = next
	}

	reg.chunkPool.Put(buf[:cap(buf)])
	if !arrayTailClosed(rawInner, prevEndAbs) {
		// The array is not terminated, but the tail may still hold one
		// complete element the splitter never emitted (it only cuts at a
		// comma or bracket). Salvage it: parse the remainder as a single
		// value and report the unterminated array.
		tail := rawInner[prevEndAbs:]
		k := 0
		for k < len(tail) && (charTable[tail[k]]&charSpace) != 0 {
			k++
		}
		if k < len(tail) && tail[k] == ',' {
			k++
			for k < len(tail) && (charTable[tail[k]]&charSpace) != 0 {
				k++
			}
		}
		if k < len(tail) && written < len(dst) {
			if _, verr := skipValueStrict(tail, k); verr == nil {
				itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(written) * structSize))
				if perr := ParseObject(tail[k:], reg, itemPtr); perr == nil {
					written++
				}
			}
		}
		problems.add(prevEndAbs, -1, fmt.Errorf("%w: unterminated array (missing closing bracket or trailing data)", ErrTypeMismatch))
	}
	return dst[:written], problems.orNil()
}

type parseTask struct {
	chunks  []Chunk
	baseIdx int
}

func UnmarshalArrayParallel[T any](raw []byte, reg *Registry, dst []T) ([]T, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Validate that the registry was built for type T using exact type identity.
	if err := validateRegistryType[T](reg); err != nil {
		return nil, fmt.Errorf("UnmarshalArrayParallel: %w", err)
	}

	startIdx := skipSpaceASM(raw, 0)
	if startIdx < 0 || startIdx >= len(raw) || raw[startIdx] != '[' {
		return nil, errors.New("expected '[' at the beginning of array")
	}
	startIdx++
	rawInner := raw[startIdx:]

	structSize := unsafe.Sizeof(*new(T))

	// If dst is empty, we must check whether the array actually has elements.
	// Scan for at least one element to distinguish "empty array" from
	// "non-empty array with insufficient destination".
	if len(dst) == 0 {
		hasElements := false
		i := 0
		for i < len(rawInner) && (charTable[rawInner[i]]&charSpace) != 0 {
			i++
		}
		if i < len(rawInner) && rawInner[i] != ']' {
			hasElements = true
		}
		if hasElements {
			return nil, fmt.Errorf("insufficient capacity: array has elements but destination is empty")
		}
		return dst[:0], nil
	}
	basePtr := unsafe.Pointer(&dst[0])

	workers := runtime.GOMAXPROCS(0)
	if workers > 16 {
		workers = 16
	}

	const batchSize = 1024
	var itemsFound int
	var offset int
	var fatalErr error
	prevElemEnd := 0
	hadElements := false

	// Fast path for single core or tiny arrays
	if workers <= 1 {
		buf := reg.chunkPool.Get().([]Chunk)
		if cap(buf) < batchSize {
			buf = make([]Chunk, batchSize)
		}
		for offset < len(rawInner) {
			count, maxDepth := findArrayElementsEarlyExitASM(rawInner[offset:], buf[:batchSize])
			if count == 0 {
				break
			}
			stepAdvance := buf[count-1].End
			if stepAdvance == 0 {
				break
			}

			validCount := 0
			for i := 0; i < count; i++ {
				if buf[i].Start != buf[i].End {
					buf[validCount].Start = buf[i].Start + offset
					buf[validCount].End = buf[i].End + offset
					validCount++
				}
			}
			if maxDepth != 0 && count < batchSize {
				fatalErr = errors.New("malformed json: unbalanced braces or brackets")
				break
			}
			for i := 0; i < validCount; i++ {
				gapStart := prevElemEnd
				expectComma := hadElements
				if !hadElements && offset > 0 {
					// resuming mid-array: the scan window starts at the
					// separating comma left by the previous batch
					gapStart = offset
					expectComma = true
				}
				if !validArrayGap(rawInner[gapStart:buf[i].Start], expectComma) {
					reg.chunkPool.Put(buf[:cap(buf)])
					fatalErr = fmt.Errorf("%w: expected ',' between array elements at %d", ErrTypeMismatch, buf[i].Start)
					break
				}
				prevElemEnd = buf[i].End
				hadElements = true
			}
			if fatalErr != nil {
				break
			}
			if validCount == 0 && !emptyArrayRemnant(rawInner[offset:]) {
				// same contract as the serial path: zero real elements is
				// only legitimate for an actually empty array remnant
				fatalErr = fmt.Errorf("%w: malformed array element (unbalanced braces or unterminated string, scanner depth %d)", ErrTypeMismatch, maxDepth)
				break
			}
			if itemsFound+validCount > len(dst) {
				fatalErr = fmt.Errorf("dst capacity insufficient: need %d", itemsFound+validCount)
				break
			}
			for i := 0; i < validCount; i++ {
				chunk := buf[i]
				itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(itemsFound+i) * structSize))
				if err := ParseObject(rawInner[chunk.Start:chunk.End], reg, itemPtr); err != nil {
					fatalErr = err
					break
				}
			}
			if fatalErr != nil {
				break
			}
			itemsFound += validCount
			offset += stepAdvance
		}
		reg.chunkPool.Put(buf[:cap(buf)])
		if fatalErr != nil {
			return nil, fatalErr
		}
		return dst[:itemsFound], nil
	}

	// PIPELINED PARALLEL PARSING
	taskCh := make(chan parseTask, workers*2)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				for i := 0; i < len(task.chunks); i++ {
					chunk := task.chunks[i]
					itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(task.baseIdx+i) * structSize))
					if err := ParseObject(rawInner[chunk.Start:chunk.End], reg, itemPtr); err != nil {
						select {
						case errCh <- err:
						default:
						}
						// Stop processing this batch on error
						break
					}
				}
				reg.chunkPool.Put(task.chunks[:cap(task.chunks)])
			}
		}()
	}

	for offset < len(rawInner) {
		select {
		case err := <-errCh:
			fatalErr = err
			goto CLEANUP
		default:
		}

		batch := reg.chunkPool.Get().([]Chunk)
		if cap(batch) < batchSize {
			batch = make([]Chunk, batchSize)
		}
		batch = batch[:batchSize]

		count, maxDepth := findArrayElementsEarlyExitASM(rawInner[offset:], batch)
		if count > len(batch) {
			count = len(batch)
		}
		if count == 0 {
			reg.chunkPool.Put(batch[:cap(batch)])
			if !emptyArrayRemnant(rawInner[offset:]) {
				fatalErr = fmt.Errorf("%w: malformed array element (unbalanced braces or unterminated string, scanner depth %d)", ErrTypeMismatch, maxDepth)
				goto CLEANUP
			}
			break
		}
		stepAdvance := batch[count-1].End
		if stepAdvance == 0 {
			reg.chunkPool.Put(batch[:cap(batch)])
			break
		}

		validCount := 0
		for i := 0; i < count; i++ {
			if batch[i].Start != batch[i].End {
				batch[validCount].Start = batch[i].Start + offset
				batch[validCount].End = batch[i].End + offset
				validCount++
			}
		}

		if maxDepth != 0 && count < batchSize {
			reg.chunkPool.Put(batch[:cap(batch)])
			fatalErr = errors.New("malformed json: unbalanced braces or brackets")
			goto CLEANUP
		}

		for i := 0; i < validCount; i++ {
			gapStart := prevElemEnd
			expectComma := hadElements
			if !hadElements && offset > 0 {
				// resuming mid-array: the scan window starts at the
				// separating comma left by the previous batch
				gapStart = offset
				expectComma = true
			}
			if !validArrayGap(rawInner[gapStart:batch[i].Start], expectComma) {
				reg.chunkPool.Put(batch[:cap(batch)])
				fatalErr = fmt.Errorf("%w: expected ',' between array elements at %d", ErrTypeMismatch, batch[i].Start)
				goto CLEANUP
			}
			prevElemEnd = batch[i].End
			hadElements = true
		}

		if itemsFound+validCount > len(dst) {
			reg.chunkPool.Put(batch[:cap(batch)])
			fatalErr = fmt.Errorf("dst capacity insufficient: need %d", itemsFound+validCount)
			goto CLEANUP
		}

		if validCount > 0 {
			taskCh <- parseTask{
				chunks:  batch[:validCount],
				baseIdx: itemsFound,
			}
			itemsFound += validCount
			offset += stepAdvance
		} else {
			reg.chunkPool.Put(batch[:cap(batch)])
			offset += stepAdvance
		}
	}

CLEANUP:
	close(taskCh)
	wg.Wait()

	if fatalErr == nil && !arrayTailClosed(rawInner, prevElemEnd) {
		fatalErr = fmt.Errorf("%w: unterminated array (missing closing bracket or trailing data)", ErrTypeMismatch)
	}

	// Check if any error was reported while waiting
	select {
	case err := <-errCh:
		if fatalErr == nil {
			fatalErr = err
		}
	default:
	}

	if fatalErr != nil {
		return nil, fatalErr
	}
	return dst[:itemsFound], nil
}
