package silentjson

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
}

// NewStreamDecoder creates a new streaming decoder.
// Note: It sets reg.CopyStrings to true to prevent zero-copy string leaks
// when the internal buffer is overwritten by the io.Reader.
func NewStreamDecoder[T any](r io.Reader, reg *Registry) *StreamDecoder[T] {
	reg.CopyStrings = true
	return &StreamDecoder[T]{
		r:   r,
		reg: reg,
		buf: make([]byte, 256*1024), // 256 KB sliding buffer
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

// nextChunk finds the next JSON object in the stream and returns its raw bytes.
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
			d.head++
			continue
		}

		// We expect an object to start here
		if d.buf[d.head] != '{' {
			return nil, fmt.Errorf("expected '{', got '%c'", d.buf[d.head])
		}

		// Find the end of the object by counting depth
		depth := 0
		endIdx := -1

	scanLoop:
		for i := d.head; i < d.tail; i++ {
			c := d.buf[i]
			switch c {
			case '"':
				i++
				for i < d.tail {
					idx := bytes.IndexByte(d.buf[i:d.tail], '"')
					if idx == -1 {
						i = d.tail
						break
					}
					i += idx

					escapes := 0
					for j := i - 1; j >= d.head && d.buf[j] == '\\'; j-- {
						escapes++
					}
					if escapes%2 == 0 {
						break // valid end quote
					}
					i++ // escaped quote, continue
				}
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					endIdx = i + 1
					break scanLoop
				}
			}
		}

		if endIdx != -1 {
			// Complete object found!
			chunk := d.buf[d.head:endIdx]
			d.head = endIdx
			return chunk, nil
		}

		// If we reached here, the object is incomplete. We need to read more.
		if d.isEOF {
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
	return ParseObject(chunk, d.reg, unsafe.Pointer(obj))
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
		if err := ParseObject(chunk, d.reg, unsafe.Pointer(&d.obj)); err != nil {
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
func (d *StreamDecoder[T]) NextRaw() ([]byte, error) {
	chunk, err := d.nextChunk()
	if err != nil {
		return nil, err
	}
	// Copy the chunk because d.buf will be overwritten by future reads
	res := make([]byte, len(chunk))
	copy(res, chunk)
	return res, nil
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
			if err := ParseObject(chunk, d.reg, unsafe.Pointer(obj)); err != nil {
				ch <- StreamResult[*T]{Err: err}
				return
			}

			ch <- StreamResult[*T]{Item: obj}
			i++
		}
	}()

	return ch
}
