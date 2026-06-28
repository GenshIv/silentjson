package silentjson

import (
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

// Decode reads the next object from the JSON array and unmarshals it into obj.
func (d *StreamDecoder[T]) Decode(obj *T) error {
	if d.hasEnded {
		return io.EOF
	}

	for {
		// Ensure we have enough data to check at least one character
		if d.head >= d.tail {
			if err := d.fill(); err != nil {
				return err
			}
			if d.head >= d.tail && d.isEOF {
				return io.EOF
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
				return fmt.Errorf("expected '[' at the beginning of the stream, got '%c'", d.buf[d.head])
			}
			d.hasStarted = true
			d.head++
			continue
		}

		// Check for end of array or comma
		if d.buf[d.head] == ']' {
			d.hasEnded = true
			return io.EOF
		}
		if d.buf[d.head] == ',' {
			d.head++
			continue
		}

		// We expect an object to start here
		if d.buf[d.head] != '{' {
			return fmt.Errorf("expected '{', got '%c'", d.buf[d.head])
		}

		// Find the end of the object by counting depth
		depth := 0
		inString := false
		var escape bool
		endIdx := -1

		for i := d.head; i < d.tail; i++ {
			c := d.buf[i]
			if inString {
				if escape {
					escape = false
				} else if c == '\\' {
					escape = true
				} else if c == '"' {
					inString = false
				}
				continue
			}

			if c == '"' {
				inString = true
				continue
			}
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
				if depth == 0 {
					endIdx = i + 1
					break
				}
			}
		}

		if endIdx != -1 {
			// Complete object found!
			chunk := d.buf[d.head:endIdx]
			d.head = endIdx
			return ParseObject(chunk, d.reg, unsafe.Pointer(obj))
		}

		// If we reached here, the object is incomplete. We need to read more.
		if d.isEOF {
			return ErrUnexpectedEOF
		}
		if err := d.fill(); err != nil {
			return err
		}
	}
}
