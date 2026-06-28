package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"

	"github.com/GenshIv/silentjson"
)

func main() {
	var inPath string
	var outPrefix string
	var limitCount int
	var limitSize int

	flag.StringVar(&inPath, "file", "", "Input JSON file containing an array (leave empty to read from stdin)")
	flag.StringVar(&outPrefix, "out", "chunk_", "Output file prefix (e.g. chunk_0.json)")
	flag.IntVar(&limitCount, "count", 0, "Max objects per sliced file")
	flag.IntVar(&limitSize, "size", 0, "Approximate max size per sliced file in bytes")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Silent-Chunker: A lightning-fast JSON array slicer powered by silentjson.\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  silent-chunker [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  1. Slice a file into chunks of 1000 objects each:\n")
		fmt.Fprintf(os.Stderr, "     silent-chunker -file input.json -count 1000 -out chunk_\n\n")
		fmt.Fprintf(os.Stderr, "  2. Slice via stdin into files of ~10MB each (Linux):\n")
		fmt.Fprintf(os.Stderr, "     cat large.json | silent-chunker -size 10485760\n\n")
		fmt.Fprintf(os.Stderr, "  3. Slice via stdin into files of ~10MB each (Windows PowerShell):\n")
		fmt.Fprintf(os.Stderr, "     Get-Content large.json | silent-chunker -size 10485760\n")
	}

	flag.Parse()

	if limitCount <= 0 && limitSize <= 0 {
		fmt.Fprintln(os.Stderr, "Error: You must specify either -count or -size > 0")
		flag.Usage()
		os.Exit(1)
	}

	var in io.Reader = os.Stdin
	if inPath != "" {
		f, err := os.Open(inPath)
		if err != nil {
			log.Fatalf("Failed to open input: %v", err)
		}
		defer f.Close()
		in = f
		log.Printf("Reading from %s...", inPath)
	} else {
		log.Printf("Reading from stdin...")
	}

	// We use an empty struct because we don't care about unmarshaling.
	// NextRaw() bypasses unmarshaling entirely.
	type Dummy struct{}
	reg := silentjson.BuildRegistry(reflect.TypeOf(Dummy{}))
	dec := silentjson.NewStreamDecoder[Dummy](in, reg)

	fileIndex := 0
	objCount := 0
	byteCount := 0
	var out *os.File
	var err error

	openNext := func() {
		if out != nil {
			out.WriteString("\n]\n")
			out.Close()
		}
		outName := fmt.Sprintf("%s%d.json", outPrefix, fileIndex)
		out, err = os.Create(outName)
		if err != nil {
			log.Fatalf("Failed to create output file: %v", err)
		}
		out.WriteString("[\n")
		fileIndex++
		objCount = 0
		byteCount = 2 // "[\n"
	}

	openNext()

	for {
		raw, err := dec.NextRaw()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Stream error during NextRaw(): %v", err)
		}

		// If we reached limits (and have at least 1 object to avoid empty files)
		if (limitCount > 0 && objCount >= limitCount) || (limitSize > 0 && byteCount > limitSize && objCount > 0) {
			openNext()
		}

		if objCount > 0 {
			out.WriteString(",\n")
			byteCount += 2
		}
		
		n, _ := out.Write(raw)
		byteCount += n
		objCount++
	}

	// Close the last file
	if out != nil {
		if objCount == 0 && fileIndex > 1 {
			// Remove empty trailing file if it happened to split perfectly on boundary
			outName := out.Name()
			out.Close()
			os.Remove(outName)
			fileIndex--
		} else {
			out.WriteString("\n]\n")
			out.Close()
		}
	}

	log.Printf("Successfully completed! Sliced into %d file(s).", fileIndex)
}
