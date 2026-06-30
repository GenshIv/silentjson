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

func UnmarshalArrayParallel[T any](raw []byte, reg *Registry, dst []T) ([]T, error) {
	if len(raw) == 0 {
		return nil, nil
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

	count, maxDepth := findArrayElementsEarlyExitASM(rawInner, buf[:len(buf)])

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

	// CHECK 1: JSON validity
	if maxDepth != 0 {
		fmt.Printf("DEBUG PARALLEL: count=%d, maxDepth=%d, rawInner=%q\n", count, maxDepth, rawInner)
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, errors.New("malformed json: unbalanced braces or brackets")
	}

	// CHECK 3: dst capacity
	if count > len(dst) {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, fmt.Errorf("dst capacity insufficient: need %d, have %d", count, len(dst))
	}
	target := dst[:count]

	if len(target) == 0 {
		reg.chunkPool.Put(buf[:cap(buf)])
		return target, nil
	}

	if err := parseArrayParallelChunks(rawInner, buf[:count], reg, unsafe.Pointer(&target[0]), unsafe.Sizeof(*new(T))); err != nil {
		reg.chunkPool.Put(buf[:cap(buf)])
		return nil, err
	}
	reg.chunkPool.Put(buf[:cap(buf)])
	return target, nil
}

func parseArrayParallelChunks(raw []byte, chunks []Chunk, reg *Registry, basePtr unsafe.Pointer, structSize uintptr) error {
	if len(chunks) == 0 {
		return nil
	}

	workers := runtime.GOMAXPROCS(0)
	const minChunksPerWorker = 128
	maxUsefulWorkers := (len(chunks) + minChunksPerWorker - 1) / minChunksPerWorker
	if workers > maxUsefulWorkers {
		workers = maxUsefulWorkers
	}
	if workers <= 1 {
		for idx := 0; idx < len(chunks); idx++ {
			chunk := chunks[idx]
			itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(idx) * structSize))
			if err := ParseObject(raw[chunk.Start:chunk.End], reg, itemPtr); err != nil {
				return err
			}
		}
		return nil
	}

	batchSize := (len(chunks) + workers - 1) / workers
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		start := w * batchSize
		if start >= len(chunks) {
			break
		}
		end := start + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			for idx := start; idx < end; idx++ {
				chunk := chunks[idx]
				itemPtr := unsafe.Pointer(uintptr(basePtr) + (uintptr(idx) * structSize))
				if err := ParseObject(raw[chunk.Start:chunk.End], reg, itemPtr); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}(start, end)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
