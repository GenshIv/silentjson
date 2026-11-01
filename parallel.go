package silentjson

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

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
	startIdx++
	rawInner := raw[startIdx:]

	count, depth := findArrayElementsEarlyExitASM(rawInner, buf[:len(buf)])
	if count < 0 || count > len(buf) {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, fmt.Errorf("asm returned invalid count: %d", count)
	}
	validCount := 0
	for i := 0; i < count; i++ {
		if buf[i].Start != buf[i].End {
			buf[validCount] = buf[i]
			validCount++
		}
	}
	count = validCount
	if count == 0 && !emptyArrayRemnant(rawInner) {
		// Zero real elements are only legitimate for an actually empty
		// array; anything else means the scanner desynchronized on
		// malformed input (the splitter emits phantom empty chunks on
		// stray commas, so this check must run after the filter).
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, fmt.Errorf("%w: malformed array element (unbalanced braces or unterminated string, scanner depth %d)", ErrTypeMismatch, depth)
	}

	if len(dst) < count {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, fmt.Errorf("insufficient capacity: need %d, have %d", count, len(dst))
	}

	target := dst[:count]
	if count == 0 {
		reg.chunkPool.Put(buf[:cap(buf)])
		return target, nil
	}
	prevEnd := 0
	for i := 0; i < count; i++ {
		if !validArrayGap(rawInner[prevEnd:buf[i].Start], i > 0) {
			reg.chunkPool.Put(buf[:cap(buf)])
			return nil, fmt.Errorf("%w: expected ',' between array elements at %d", ErrTypeMismatch, buf[i].Start)
		}
		prevEnd = buf[i].End
	}
	structSize := unsafe.Sizeof(*new(T))
	basePtr := unsafe.Pointer(&target[0])

	var err error
	for i := 0; i < count; i++ {
		chunk := buf[i]
		if chunk.Start < 0 || chunk.End > len(raw) || chunk.Start >= chunk.End {
			err = fmt.Errorf("invalid json boundaries at chunk %d", i)
			break
		}

		itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(i) * structSize))

		if err = ParseObject(rawInner[chunk.Start:chunk.End], reg, itemPtr); err != nil {
			break
		}
	}

	reg.chunkPool.Put(buf[:cap(buf)])
	if err != nil {
		return nil, err
	}
	if !arrayTailClosed(rawInner, buf[count-1].End) {
		return nil, fmt.Errorf("%w: unterminated array (missing closing bracket or trailing data)", ErrTypeMismatch)
	}
	return target, nil
}

type parseTask struct {
	chunks  []Chunk
	baseIdx int
}

func UnmarshalArrayParallel[T any](raw []byte, reg *Registry, dst []T) ([]T, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	startIdx := skipSpaceASM(raw, 0)
	if startIdx < 0 || startIdx >= len(raw) || raw[startIdx] != '[' {
		return nil, errors.New("expected '[' at the beginning of array")
	}
	startIdx++
	rawInner := raw[startIdx:]

	structSize := unsafe.Sizeof(*new(T))
	if len(dst) == 0 {
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
