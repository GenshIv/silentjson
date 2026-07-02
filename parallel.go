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

	count, _ := findArrayElementsEarlyExitASM(rawInner, buf[:len(buf)])
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

	if len(dst) < count {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, fmt.Errorf("insufficient capacity: need %d, have %d", count, len(dst))
	}

	target := dst[:count]
	if count == 0 {
		reg.chunkPool.Put(buf[:cap(buf)])
		return target, nil
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
