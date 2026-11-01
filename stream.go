package silentjson

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"unsafe"
)

var (
	ErrStreamEOF = errors.New("stream reached EOF")
)

// StreamDecoder reads a JSON array incrementally from an io.Reader.
type StreamDecoder[T any] struct {
	r          io.Reader
	reg        *Registry
	buf        []byte
	head       int
	tail       int
	isEOF      bool
	hasStarted bool
	hasEnded   bool
	obj        T // Reusable internal object for zero-allocation Next()
	// copyStrings is always true for streams: the internal buffer is
	// overwritten by the io.Reader, so zero-copy aliases would dangle.
	// P1-4 fix: it is per-decoder state, NOT a mutation of the shared registry.
	copyStrings bool
	// deliveredFirst is true once the first element has been handed out.
	// A separating comma is only legal between two elements, so the
	// comma-skip branches consult this flag to reject leading commas.
	deliveredFirst bool
	// afterComma is true between a skipped separating comma and the next
	// delivered element: a second comma in that window is a duplicate
	// separator and must be rejected.
	afterComma bool
}

// NewStreamDecoder creates a new streaming decoder.
// It does NOT modify the passed registry (the registry may be shared with
// direct ParseObject callers); copy-on-read string semantics are applied
// per-decoder instead.
func NewStreamDecoder[T any](r io.Reader, reg *Registry) *StreamDecoder[T] {
	return &StreamDecoder[T]{
		r:           r,
		reg:         reg,
		copyStrings: true,
		buf:         make([]byte, 256*1024), // 256 KB sliding buffer
	}
}

// fill reads more data from the reader into the buffer.
func (d *StreamDecoder[T]) fill() error {
	if d.isEOF {
		return nil
	}

	// If buffer is too small to hold a reasonable object, we might need to grow it,
	// but for now, we just slide the remaining data to the beginning.
	if d.head > 0 {
		copy(d.buf, d.buf[d.head:d.tail])
		d.tail -= d.head
		d.head = 0
	}

	// If buffer is completely full of one massive object, we must grow it.
	if d.tail == len(d.buf) {
		newBuf := make([]byte, len(d.buf)*2)
		copy(newBuf, d.buf[:d.tail])
		d.buf = newBuf
	}

	n, err := d.r.Read(d.buf[d.tail:])
	d.tail += n
	if err != nil {
		if err == io.EOF {
			d.isEOF = true
			return nil
		}
		return err
	}
	return nil
}

// nextChunk finds the next JSON value in the stream and returns its raw bytes.
// Supports objects, arrays, strings, numbers, booleans, and null.
// The returned slice points to the internal buffer and is only valid until the next read.
func (d *StreamDecoder[T]) nextChunk() ([]byte, error) {
	if d.hasEnded {
		return nil, io.EOF
	}

	for {
		// Ensure we have enough data to check at least one character
		if d.head >= d.tail {
			if err := d.fill(); err != nil {
				return nil, err
			}
			if d.head >= d.tail && d.isEOF {
				return nil, io.EOF
			}
		}

		// Skip whitespace
		for d.head < d.tail && (charTable[d.buf[d.head]]&charSpace) != 0 {
			d.head++
		}

		if d.head >= d.tail {
			continue // Need more data
		}

		// Initial array start
		if !d.hasStarted {
			if d.buf[d.head] != '[' {
				return nil, fmt.Errorf("expected '[' at the beginning of the stream, got '%c'", d.buf[d.head])
			}
			d.hasStarted = true
			d.head++
			continue
		}

		// Check for end of array or comma
		if d.buf[d.head] == ']' {
			d.hasEnded = true
			return nil, io.EOF
		}
		if d.buf[d.head] == ',' {
			// A comma is only legal BETWEEN two elements: leading (before
			// anything was delivered), trailing (followed by ']') and
			// doubled are malformed and must be rejected, not skipped.
			if !d.deliveredFirst {
				return nil, fmt.Errorf("%w: leading comma at %d", ErrTypeMismatch, d.head)
			}
			if d.afterComma {
				return nil, fmt.Errorf("%w: duplicate comma at %d", ErrTypeMismatch, d.head)
			}
			d.afterComma = true
			d.head++
			for d.head < d.tail && (charTable[d.buf[d.head]]&charSpace) != 0 {
				d.head++
			}
			if d.head < d.tail && d.buf[d.head] == ']' {
				return nil, fmt.Errorf("%w: trailing comma at %d", ErrTypeMismatch, d.head-1)
			}
			continue
		}

		// Dispatch using AVX2 unified array element chunker
		var chunkBuf [1]Chunk
		count, _ := findArrayElementsEarlyExitASM(d.buf[d.head:d.tail], chunkBuf[:])

		if count > 0 && chunkBuf[0].End > 0 {
			// Complete element found!
			// chunkBuf[0].End is the index of the comma or closing bracket
			endIdx := d.head + chunkBuf[0].End
			d.deliveredFirst = true
			d.afterComma = false
			chunk := d.buf[d.head:endIdx]

			// If it ended on a bracket, NextRawBlock/nextChunk handles it by seeing ']' on next call
			// For comma, we skip it by pointing d.head exactly at it so the next iteration skips it.
			d.head = endIdx
			return chunk, nil
		}

		// If we reached here, the element is incomplete in the current buffer.
		if d.isEOF {
			// EOF with leftover data: the splitter could not find one
			// complete element (no trailing comma or bracket), so the
			// remainder may be either a VALID last element cut off from
			// its terminator, or a stream truncated in the middle of a
			// value. Validate the fragment grammatically: hand it out
			// only if it is exactly one complete JSON value; report a
			// clean truncation error otherwise (the old behavior handed
			// malformed fragments out as records).
			j := d.head
			for j < d.tail && (charTable[d.buf[j]]&charSpace) != 0 {
				j++
			}
			if j < d.tail {
				frag := d.buf[j:d.tail]
				end, err := skipValueStrict(frag, 0)
				if err == nil && end == len(frag) {
					d.head = d.tail
					return frag, nil
				}
				d.head = d.tail
				// Partial fragment: hand the bytes out together with the
				// truncation error, io.Reader-style (n > 0, err != nil).
				// A stream consumer may need the tail (e.g. to resume
				// after a reconnect); callers that only want complete
				// records just check the error first.
				return frag, fmt.Errorf("%w: stream truncated inside a value", ErrUnexpectedEOF)
			}
			d.head = d.tail
			return nil, ErrUnexpectedEOF
		}
		if err := d.fill(); err != nil {
			return nil, err
		}
	}
}

// Decode reads the next object from the JSON array and unmarshals it into obj.
func (d *StreamDecoder[T]) Decode(obj *T) error {
	chunk, err := d.nextChunk()
	if err != nil {
		return err
	}
	return parseObjectStrict(chunk, d.reg, unsafe.Pointer(obj), d.copyStrings)
}

// Next processes the remainder of the JSON array, unmarshaling each object into an internal reusable instance
// and passing it to the provided callback. This enables strictly zero-allocation stream processing.
// If the callback returns false, the iteration stops early.
func (d *StreamDecoder[T]) Next(cb func(obj *T) bool) error {
	for {
		chunk, err := d.nextChunk()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := parseObjectStrict(chunk, d.reg, unsafe.Pointer(&d.obj), d.copyStrings); err != nil {
			return err
		}
		if !cb(&d.obj) {
			return nil
		}
	}
}

// NextRaw reads the next object from the JSON array and returns its raw JSON bytes.
// This completely skips the unmarshaling phase, providing extreme throughput for raw extraction.
// A copy of the underlying bytes is returned to ensure memory safety.
// NextRaw returns the raw bytes of the next object in the stream.
// On a stream truncated inside a value it returns the partial fragment
// TOGETHER with the truncation error (io.Reader semantics): consumers
// that need the tail bytes (e.g. to resume after a reconnect) have
// them, consumers that only want complete records check the error.
func (d *StreamDecoder[T]) NextRaw() ([]byte, error) {
	chunk, err := d.nextChunk()
	if err != nil && len(chunk) == 0 {
		return nil, err
	}
	// Copy the chunk because d.buf will be overwritten by future reads
	res := make([]byte, len(chunk))
	copy(res, chunk)
	return res, err
}

// StreamResult holds the result of an asynchronous stream parse operation.

// StreamResult holds the result of an asynchronous stream parse operation.
type StreamResult[T any] struct {
	Item T
	Err  error
}

// NextChan launches a background goroutine that parses objects and sends them to the returned channel.
// It uses a Ring Buffer of size `bufferSize` to achieve ZERO allocations during streaming.
// WARNING: The returned pointer is reused after `bufferSize` iterations. You must not retain
// references to it or its slices indefinitely.
func (d *StreamDecoder[T]) NextChan(bufferSize int) <-chan StreamResult[*T] {
	if bufferSize < 1 {
		bufferSize = 1
	}
	ch := make(chan StreamResult[*T], bufferSize)
	ringSize := bufferSize + 4
	ring := make([]T, ringSize)

	go func() {
		defer close(ch)
		i := 0
		for {
			chunk, err := d.nextChunk()
			if err != nil {
				if err != io.EOF {
					ch <- StreamResult[*T]{Err: err}
				}
				return
			}

			// Zero-allocation ring buffer reuse
			obj := &ring[i%ringSize]
			if err := parseObjectStrict(chunk, d.reg, unsafe.Pointer(obj), d.copyStrings); err != nil {
				ch <- StreamResult[*T]{Err: err}
				return
			}

			ch <- StreamResult[*T]{Item: obj}
			i++
		}
	}()

	return ch
}

type streamBatch[T any] struct {
	raw     []byte
	count   int
	baseIdx int
	done    chan error
}

// NextChanParallel launches a worker pool to parse the JSON stream concurrently.
// It uses double buffering for raw IO data, eliminating pauses between disk reads and CPU parsing.
// The objects are guaranteed to be returned in the original JSON array order.
func (d *StreamDecoder[T]) NextChanParallel(workers int, bufferSize int) <-chan StreamResult[*T] {
	if workers < 1 {
		workers = 1
	}
	if bufferSize < 1 {
		bufferSize = 1
	}

	ch := make(chan StreamResult[*T], bufferSize)

	const batchSize = 64
	const maxRawSize = 1024 * 1024 // 1MB max raw slice per batch
	rawRingSize := workers * 2

	// ringSize must hold bufferSize + max possible in-flight items + padding
	ringSize := bufferSize + (rawRingSize+2)*batchSize + 16
	ring := make([]T, ringSize)

	// Double buffering pool
	rawPool := &sync.Pool{
		New: func() interface{} {
			return make([]byte, maxRawSize)
		},
	}

	taskCh := make(chan streamBatch[T], rawRingSize)

	// Launch workers
	for w := 0; w < workers; w++ {
		go func() {
			for task := range taskCh {
				if d.isEOF && task.count == 0 { // Just in case
					task.done <- nil
					continue
				}

				chunks := d.reg.chunkPool.Get().([]Chunk)
				if cap(chunks) < task.count {
					chunks = make([]Chunk, task.count)
				}
				chunks = chunks[:task.count]

				c, _ := findArrayElementsEarlyExitASM(task.raw, chunks)
				if c > len(chunks) {
					c = len(chunks)
				}

				var err error
				for i := 0; i < c; i++ {
					obj := &ring[(task.baseIdx+i)%ringSize]
					chunkRaw := task.raw[chunks[i].Start:chunks[i].End]
					if parseErr := parseObjectStrict(chunkRaw, d.reg, unsafe.Pointer(obj), d.copyStrings); parseErr != nil {
						err = parseErr
						break
					}
				}
				d.reg.chunkPool.Put(chunks[:cap(chunks)])
				task.done <- err
				rawPool.Put(task.raw[:cap(task.raw)]) // Return buffer to pool
			}
		}()
	}

	// Producer and Submitter
	go func() {
		defer close(taskCh)

		batchQueue := make(chan streamBatch[T], rawRingSize)

		// Submitter
		go func() {
			defer close(ch)
			for batch := range batchQueue {
				err := <-batch.done
				if err != nil {
					ch <- StreamResult[*T]{Err: err}
					return // stop Submitter
				}
				for i := 0; i < batch.count; i++ {
					ch <- StreamResult[*T]{Item: &ring[(batch.baseIdx+i)%ringSize]}
				}
			}
		}()

		itemsFound := 0
		rawIdx := 0
		for {
			rawChunk, count, err := d.NextRawBlock(batchSize, maxRawSize)
			if err != nil && err != io.EOF {
				done := make(chan error, 1)
				done <- err
				batchQueue <- streamBatch[T]{done: done}
				return
			}
			if count == 0 {
				break
			}

			myRaw := rawPool.Get().([]byte)
			if len(rawChunk) > cap(myRaw) {
				myRaw = make([]byte, len(rawChunk)*2)
			}
			myRaw = myRaw[:cap(myRaw)]
			copy(myRaw, rawChunk)
			myRaw = myRaw[:len(rawChunk)]

			done := make(chan error, 1)
			batch := streamBatch[T]{
				raw:     myRaw,
				count:   count,
				baseIdx: itemsFound,
				done:    done,
			}
			itemsFound += count
			rawIdx++

			batchQueue <- batch
			taskCh <- batch

			if err == io.EOF {
				break
			}
		}
		close(batchQueue)
	}()

	return ch
}

// NextRawBlock reads multiple objects from the JSON array and returns a single raw slice containing them all.
// The slice points to the internal buffer and is only valid until the next read.
// maxCount: maximum number of objects to extract. 0 means no limit.
// maxSize: maximum approximate size in bytes. 0 means no limit.
// It returns (rawChunk, objectCount, error).
func (d *StreamDecoder[T]) NextRawBlock(maxCount int, maxSize int) ([]byte, int, error) {
	if d.hasEnded {
		return nil, 0, io.EOF
	}

	for {
		if d.head >= d.tail {
			if err := d.fill(); err != nil {
				return nil, 0, err
			}
			if d.head >= d.tail && d.isEOF {
				return nil, 0, io.EOF
			}
		}

		for d.head < d.tail && (charTable[d.buf[d.head]]&charSpace) != 0 {
			d.head++
		}

		if d.head >= d.tail {
			continue
		}

		if !d.hasStarted {
			if d.buf[d.head] != '[' {
				return nil, 0, fmt.Errorf("expected '[' at the beginning of the stream, got '%c'", d.buf[d.head])
			}
			d.hasStarted = true
			d.head++
			continue
		}

		if d.buf[d.head] == ']' {
			d.hasEnded = true
			return nil, 0, io.EOF
		}

		// Skip the separating comma if present, with the same rules as
		// nextChunk: only between two elements, never leading or trailing.
		if d.buf[d.head] == ',' {
			if !d.deliveredFirst {
				return nil, 0, fmt.Errorf("%w: leading comma at %d", ErrTypeMismatch, d.head)
			}
			if d.afterComma {
				return nil, 0, fmt.Errorf("%w: duplicate comma at %d", ErrTypeMismatch, d.head)
			}
			d.afterComma = true
			d.head++
			for d.head < d.tail && (charTable[d.buf[d.head]]&charSpace) != 0 {
				d.head++
			}
			if d.head >= d.tail {
				continue
			}
			if d.buf[d.head] == ']' {
				return nil, 0, fmt.Errorf("%w: trailing comma at %d", ErrTypeMismatch, d.head-1)
			}
		}

		// Use AVX2 bulk element detection for all types
		chunkSize := maxCount
		if chunkSize <= 0 || chunkSize > 10000 {
			chunkSize = 10000 // reasonable batch capacity
		}

		// Re-use chunk pool to prevent allocations
		chunks := d.reg.chunkPool.Get().([]Chunk)
		if cap(chunks) < chunkSize {
			chunks = make([]Chunk, chunkSize)
		}

		// We pass the full available buffer.
		// findArrayElementsEarlyExitASM will stop when it fills chunks[:chunkSize].
		dataToScan := d.buf[d.head:d.tail]

		count, _ := findArrayElementsEarlyExitASM(dataToScan, chunks[:chunkSize])

		if count == 0 && d.tail-d.head > 1024*1024 { // Fallback if 1 huge object
			count, _ = findArrayElementsEarlyExitASM(d.buf[d.head:d.tail], chunks[:1])
		}

		if count > 0 {
			// Filter phantom empty chunks (stray commas) out of the count
			// and validate the separators between the real elements.
			real := 0
			prevEnd := 0
			for i := 0; i < count; i++ {
				if chunks[i].Start == chunks[i].End {
					continue
				}
				// Separators BETWEEN scan windows are consumed by the
				// comma branch before the window starts, so only gaps
				// between elements INSIDE the window expect a comma here.
				expectComma := real > 0
				gapStart := prevEnd
				if !validArrayGap(dataToScan[gapStart:chunks[i].Start], expectComma) {
					d.reg.chunkPool.Put(chunks[:cap(chunks)])
					return nil, 0, fmt.Errorf("%w: expected ',' between array elements at %d", ErrTypeMismatch, d.head+chunks[i].Start)
				}
				prevEnd = chunks[i].End
				chunks[real] = chunks[i]
				real++
			}
			if real == 0 {
				d.reg.chunkPool.Put(chunks[:cap(chunks)])
				if d.isEOF {
					return nil, 0, ErrUnexpectedEOF
				}
				if err := d.fill(); err != nil {
					return nil, 0, err
				}
				continue
			}
			// Complete elements found!
			endIdx := d.head + chunks[real-1].End
			res := d.buf[d.head:endIdx]
			d.head = endIdx
			d.deliveredFirst = true
			d.afterComma = false
			d.reg.chunkPool.Put(chunks[:cap(chunks)])
			return res, real, nil
		}
		d.reg.chunkPool.Put(chunks[:cap(chunks)])

		// If we reach here, we couldn't find a complete element within the available data
		if d.isEOF {
			return nil, 0, ErrUnexpectedEOF
		}
		if err := d.fill(); err != nil {
			return nil, 0, err
		}
	}
}
