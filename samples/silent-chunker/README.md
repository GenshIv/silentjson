# Silent-Chunker: A Lightning-Fast JSON Array Slicer

`silent-chunker` is a command-line utility built on top of `silentjson`. It demonstrates the raw power of the `NextRawBlock()` stream decoding feature.

## What it does

If you have a massive JSON file containing a huge array of objects (e.g., a 10GB database dump), trying to parse or load it all into memory at once is impossible.

`silent-chunker` reads this massive stream (from a file or `stdin`) and efficiently slices it into smaller, manageable JSON files (chunks) based on a specified limit (either by number of objects or approximate byte size).

**Crucially, it does this WITHOUT unmarshaling.** It uses `NextRawBlock()` to rapidly scan the JSON structure, identify object boundaries using SIMD/CL-MUL, and extract the raw byte chunks directly. This allows it to slice files at speeds exceeding **4 GB/s** with near-zero allocations.

## Building

```bash
go build -o silent-chunker main.go
```

## Usage

```bash
Usage:
  silent-chunker [flags]

Flags:
  -count int
        Max objects per sliced file
  -file string
        Input JSON file containing an array (leave empty to read from stdin)
  -out string
        Output file prefix (e.g. chunk_0.json) (default "chunk_")
  -size int
        Approximate max size per sliced file in bytes
```

### Examples

**1. Slice a file by object count:**
Slice a massive file into chunks containing exactly 1,000 objects each.
```bash
./silent-chunker -file input.json -count 1000 -out chunk_
```

**2. Slice a stream by file size (Linux/macOS):**
Stream a huge file through `stdin` and split it into files of approximately 10MB each.
```bash
cat large.json | ./silent-chunker -size 10485760
```

**3. Slice a stream by file size (Windows PowerShell):**
```powershell
Get-Content large.json | .\silent-chunker.exe -size 10485760
```

## Why this example is important

This code demonstrates how to use `silentjson`'s advanced streaming capabilities not just for mapping data to Go structs, but for high-speed raw data extraction and routing. By passing an empty struct `type Dummy struct{}` to the Registry, we bypass the mapping layer entirely, letting the internal scanner fly through the input stream.
