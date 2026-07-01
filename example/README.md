# SilentJSON Quickstart Example

This directory contains a basic example demonstrating the core features of `silentjson`. 

It covers two primary use cases:
1. **Parallel Unmarshaling**: Reading an entire JSON array into memory extremely fast using `UnmarshalArrayParallel`.
2. **Stream Parsing**: Reading a JSON array from an `io.Reader` using `NewStreamDecoder` and iterating over it with `Next()`.

## How to run

```bash
go run main.go
```

## What you will learn

* **Registry Initialization:** How to use `silentjson.BuildRegistry(reflect.TypeOf(...))` once at the application startup to avoid reflection overhead during actual parsing.
* **Zero-Allocation Slices:** Why `silentjson` requires you to provide a pre-allocated slice (e.g., `make([]Employee, 2)`).
* **Streaming Iterator:** How `decoder.Next()` reuses a single struct pointer inside the callback, ensuring zero allocations during streaming.
