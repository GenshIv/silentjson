# Silent-Chunker

`silent-chunker` is a lightning-fast CLI utility built on top of `silentjson` to safely slice large JSON arrays into smaller files. It does not parse the objects into memory (zero-allocation for mapping), operating via a highly optimized stream scanner. This makes it capable of slicing gigabytes of JSON almost instantaneously.

## Features
- **Memory Efficient**: Uses a small sliding buffer, so it can process 100GB files on a machine with 512MB RAM.
- **Fast**: Extracts raw objects directly using boundary scanners without heavy deserialization.
- **Flexible**: Can limit the output size by **number of objects** or by **approximate byte size**.
- **Pipelining**: Reads from `stdin` natively, allowing you to chain it with other tools.

## Installation / Compilation

To build the executable:

```bash
go build -o silent-chunker main.go
```

## Usage

```text
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

### Examples (Windows)

1. **Slice a large file into files with 5,000 objects each:**
   ```powershell
   .\silent-chunker.exe -file large_data.json -count 5000 -out "my_data_part_"
   ```

2. **Pipeline data and slice into files of ~50MB each:**
   ```powershell
   Get-Content large_data.json | .\silent-chunker.exe -size 52428800
   ```
   *(Note: `Get-Content` in PowerShell might add memory overhead. Using the `-file` flag directly is faster).*

### Examples (Linux / macOS)

1. **Slice a large file into files of ~10MB each:**
   ```bash
   ./silent-chunker -file large_data.json -size 10485760
   ```

2. **Pipeline stream via `cat` or `curl`:**
   ```bash
   cat huge.json | ./silent-chunker -count 1000 -out "split_"
   
   # Or directly from the network!
   curl http://example.com/api/large-dump | ./silent-chunker -size 1048576
   ```
