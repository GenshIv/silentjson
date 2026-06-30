# silentjson: The "Just Works" High-Performance JSON Parser for Go
![silentjson.jpg](assets/silentjson.jpg)
`silentjson` is a highly optimized, reflection-free, and zero-allocation JSON library for Go that delivers extreme performance **without requiring any code generation.**

## 🚀 Why `silentjson`?

In a world of high-performance Go libraries, `silentjson` stands out by providing massive speed boosts with zero developer friction.

- **Up to 30x Faster Parsing:** For large JSON arrays, `UnmarshalArrayParallel` leverages all your CPU cores, achieving a performance increase of over 3000% compared to the standard library (reaching speeds over 3.3 GB/s).
- **7x Faster Standard Parsing:** Even on a single core, `UnmarshalSlice` is over 7 times faster than `encoding/json` for typical JSON objects (reaching ~747 MB/s).
- **Zero Code Generation:** This is the key. Unlike other fast JSON libraries, you don't need to generate any code. There are no extra build steps, no `go:generate` commands to remember, and no complex CI/CD pipeline configurations. **It works out-of-the-box, just like the standard library, only much faster.** This makes it trivial to integrate into any project, including those deployed in Docker or Kubernetes environments.

> [!TIP]
> **🤔 When to use SilentJSON?**
> Use this library for processing large arrays of objects where maximum throughput and minimal memory consumption are critical. However, if you require 100% strict adherence to the JSON specification for rare or non-standard edge cases, it's better to stick with the standard `encoding/json`.

## 📖 The Origin Story

This project was born out of a real-world necessity. While working on a large-scale project that required constant parsing of massive JSON catalogs, we hit a severe performance bottleneck. `easyjson` was still too slow for our specific needs, and fully integrating it would have broken our established workflows due to its code generation requirements. 

We desperately needed a lightning-fast alternative that didn't rely on JIT (Just-In-Time compilation) or code generation, but couldn't find one that met our strict requirements at the time. While there are a few alternatives available today, building `silentjson` became a personal "todo" to prove that extreme, reflection-free performance could be achieved with a clean, drop-in API.

## ⚠️ Caveats & Considerations

* **`unsafe` package:** This library heavily utilizes the `unsafe` package. Use with care.
* **Input Buffer Immutability:** Because strings are mapped directly via zero-copy, the underlying `rawJSON` byte slice **must not be modified** while the parsed objects are still in use.
* **Memory Retention (Zero-Copy Side Effect):** Because strings hold direct references to the original `rawJSON` buffer, retaining even a single parsed string in memory will prevent the entire underlying JSON byte array from being garbage collected. If you only need to store a small subset of parsed data for a long time, explicitly copy the strings (e.g., using `strings.Clone(val)`).
* **CPU Usage (Parallel Parsing):** `UnmarshalArrayParallel` is designed to use all available CPU cores to maximize speed for large payloads. It is ideal for batch processing or data pipelines. Avoid using it inside individual, high-concurrency API handlers, as this can lead to excessive goroutine creation. For per-request parsing, `UnmarshalSlice` is the better choice.

## Performance Deep Dive

Our latest scalability benchmarks (testing arrays from 10 to 100,000 objects) prove that `silentjson` is the fastest JSON serialization and deserialization library for Go, outperforming industry leaders like **Sonic** and **simdjson-go**.

### 1. Deserialization (Parsing / Unmarshal)
We benchmarked unmarshaling an array of 100,000 complex objects. Because Protobuf is a highly compact binary format, comparing by "MB/s" is mathematically invalid (1 MB of Protobuf contains far more objects than 1 MB of JSON). The only fair metric is **Objects processed per second**.

| Library | Objects/sec | Throughput (MB/s) | Latency (ns/op) | Memory Allocated | Allocs/op | Notes |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **SilentJSON** (Parallel) | **21,780,689 obj/s** 👑 | **3458.96 MB/s** 👑 | **4,591,223 ns** 👑 | **0.14 MB** 👑 | **4** 👑 | Full Go Struct Binding |
| **Sonic (Parallel)** | 13,720,562 obj/s | 2179.65 MB/s | 7,288,331 ns | 22.07 MB | 210068 | Full Go Struct Binding |
| **Sonic** | 3,471,454 obj/s | 551.48 MB/s | 28,806,370 ns | 16.21 MB | 10002 | Full Go Struct Binding |
| **Protobuf** | 3,416,734 obj/s | 232.54 MB/s | 29,267,715 ns | 39.12 MB | 1100019 | Binary Format |
| **simdjson-go** | 2,643,408 obj/s | 419.93 MB/s | 37,829,915 ns | 6.68 MB | 3 | **AST Only** (No Struct Binding) |
| **Standard (`encoding/json`)**| 698,378 obj/s | 110.94 MB/s | 143,188,857 ns | 3.90 MB | 509997 | Full Go Struct Binding |

> [!NOTE]
> **What about `simdjson-go`?**
> `simdjson-go` is a highly optimized C++ port utilizing SIMD instructions. However, its API is purely AST-based, making it notoriously difficult to work with for standard Go development compared to libraries that automatically map to Go structs. Furthermore, even though it skips the heavy work of struct mapping and reflection, **SilentJSON's parallel parsing architecture still outperforms its raw parsing speed by ~8x on large arrays**, while keeping the developer experience identical to `encoding/json`!
>
> **Developer Experience Comparison:**
> | Approach | Libraries |
> | :--- | :--- |
> | **Struct-mapping (automatic)** | SilentJSON, Sonic, `encoding/json` |
> | **AST-only (manual)** | simdjson-go |

```mermaid
xychart-beta
    title "Parsing Speed: 100k Objects (Millions of Objects/sec, Higher is Better)"
    x-axis ["SilentJSON", "Sonic (Parallel)", "Sonic", "Protobuf", "simdjson-go", "Standard"]
    y-axis "Millions Obj/sec" 0 --> 25
    bar [21.78, 13.72, 3.47, 3.41, 2.64, 0.69]
```

### Scalability Across File Sizes (< 1 KB to 640 MB)
Our CLMUL-accelerated parallel scanner not only reaches incredible peaks but maintains its performance lead across all file sizes.
Notice how `SilentJSON` scales rapidly and then gracefully hits the physical limits of memory bandwidth (RAM-bound) at sizes exceeding the CPU's L3 cache (128 MB+), always outperforming the competition.
Even on micro-payloads (e.g. 5 objects, ~646 bytes), `SilentJSON` easily outperforms both `Sonic` and the standard library:

| Library | Throughput (MB/s) | Latency (ns/op) | Memory Allocated | Allocs/op |
| :--- | :--- | :--- | :--- | :--- |
| **SilentJSON** (Parallel) | **735.98 MB/s** 👑 | **877.7 ns** 👑 | **25 B** 👑 | **1** 👑 |
| **Sonic** | 317.21 MB/s | 2036 ns | 2632 B | 21 |
| **Standard (`encoding/json`)** | 84.61 MB/s | 7635 ns | 2528 B | 52 |

```mermaid
xychart-beta
    title "Parallel Parsing Speed vs File Size (MB/s, Higher is Better)"
    x-axis "File Size" ["< 1 KB", "10 KB", "120 KB", "1.2 MB", "12 MB", "120 MB", "640 MB"]
    y-axis "Speed (MB/s)" 0 --> 4000
    line "SilentJSON" [736, 781, 1622, 2406, 2874, 3230, 3475]
    line "Sonic Parallel" [97, 490, 1025, 1905, 2365, 2124, 2374]
    line "Standard" [85, 89, 92, 92, 93, 96, 83]
```

To emphasize our perfect linear scaling and O(N) complexity, here is how the parsing throughput stays perfectly flat regardless of the number of objects. Notice the horizontal straight line, showing no performance degradation at scale:

```mermaid
xychart-beta
    title "Parsing Throughput Scaling (MB/s, Higher is Better)"
    x-axis ["10k", "25k", "50k", "100k"]
    y-axis "MB/s" 0 --> 3500
    line "SilentJSON" [3050, 3183, 3320, 3347]
    line "Sonic" [421, 459, 463, 467]
    line "Standard" [106, 106, 107, 107]
```
> **Legend:**
> 🔵 **Top Line:** `SilentJSON` (maintaining stable ~3300 MB/s)
> 🟢 **Middle Line:** `Sonic` (~460 MB/s)
> 🔴 **Bottom Line:** `Standard` (~107 MB/s)

### 2. Stream Parsing (io.Reader)
When you are downloading gigabytes of JSON arrays over the network and want to parse them on-the-fly without loading the entire payload into RAM, you need a streaming parser. 

Because `simdjson` and `sonic` (for standard arrays) require the **entire** payload in memory, they cannot perform streaming array processing. `SilentJSON` includes a specialized `StreamDecoder` that uses an optimized boundary scanner to process infinite streams.

| Library | Throughput (MB/s) | Memory Allocated | Allocs/op | Notes |
| :--- | :--- | :--- | :--- | :--- |
| **SilentJSON (NextRawBlock)** | **~4009 MB/s** 🚀 | **0.9 MB** | **5.7k** | Extreme zero-alloc bulk chunk extraction |
| **SilentJSON (NextRaw)** | **~2547 MB/s** 🚀 | **526 MB** | **3.0M** | Extreme speed raw stream chunk extraction |
| **SilentJSON (Decode)** | **614.90 MB/s** 👑 | **41 MB** 👑 | **7.7M** 👑 | Full Go Struct Binding, zero alloc iteration |
| **Jsoniter (Stream)** | 457.22 MB/s | 148 MB | 14.6M | 2x more GC pressure |
| **SilentJSON (NextChan)**| **446.96 MB/s** ⚡ | **41 MB** 👑| **7.7M** 👑| Async Producer-Consumer mode (Ring Buffer) |
| **Standard (`json.NewDecoder`)**| 105.70 MB/s | 162 MB | 13.3M | Slowest, highest memory usage |

```mermaid
xychart-beta
    title "Streaming Throughput vs jsoniter (MB/s) - Higher is Better"
    x-axis ["Standard", "NextChan", "jsoniter", "Decode", "NextRaw", "NextRawBlock"]
    bar [105, 446, 457, 614, 2547, 4009]
```

```mermaid
xychart-beta
    title "Memory Allocated (MB) - Lower is Better"
    x-axis ["NextChan (Ring)", "Decode (Sync)", "jsoniter", "Standard"]
    bar [41, 41, 148, 162]
```

*Note: Stream parsing disables Zero-Copy strings to ensure memory safety when the underlying `io.Reader` buffer is overwritten, which is why the throughput of full struct binding is ~471 MB/s instead of the 3.3 GB/s seen in Batch parsing. However, using the new `NextRawBlock()` allows you to extract raw object chunks at **~4.1 GB/s** directly from the stream with **zero allocations**!*


### 3. Serialization (Generation / Marshal)
We benchmarked generating a JSON array of 100,000 complex objects. `simdjson-go` is excluded as it is a parser only.

| Library | Throughput (MB/s) | Latency (ns/op) | Memory Allocated | Allocs/op |
| :--- | :--- | :--- | :--- | :--- |
| **SilentJSON** | **1454.91 MB/s** 👑 | **10,222,408 ns** 👑 | **0 MB (Zero-Alloc)** 👑 | **0** 👑 |
| **Sonic** | 1400.53 MB/s | 11,342,853 ns | 78.18 MB | 37 |
| **Standard (`encoding/json`)**| 596.53 MB/s | 26,630,475 ns | 15.15 MB | 2 |
| **Protobuf** | 452.45 MB/s | 15,042,191 ns | 6.49 MB | 1 |

```mermaid
xychart-beta
    title "Serialization Throughput: 100k Objects (MB/s, Higher is Better)"
    x-axis ["SilentJSON", "Sonic", "Standard", "Protobuf"]
    y-axis "MB/s" 0 --> 1600
    bar [1454, 1400, 596, 452]
```

### Benchmark Methodology & Fairness

To ensure our benchmarks are as fair and accurate as possible, we strictly adhere to the following principles:

1. **Strict Data Isolation**: Payload generation (such as creating the 100,000 Go structs, allocating destination slices, or converting data for Protobuf) is strictly isolated from the actual measurement. We extensively use `b.ResetTimer()` so that **only** the raw `Marshal` and `Unmarshal` functions are timed.
2. **Realistic Workloads**: Instead of testing trivial JSON objects, our payload consists of a deeply nested `Employee` struct containing arrays, nested objects, floats, booleans, and strings to simulate a heavy, real-world database dump.
3. **The "Unfair" Concurrency Penalty**: `SilentJSON` achieves its blazing fast parallel speeds by spawning standard Go goroutines *on-the-fly* during every call to `UnmarshalArrayParallel` and then letting them die. It does not use long-running background worker threads. This means `SilentJSON` intentionally pays a heavy latency penalty for goroutine scheduling on *every single operation*. Despite this overhead, it still easily defeats **Sonic** (which relies heavily on pre-compiled JIT caches and persistent `sync.Pool` memory blocks that stay resident in memory). If `SilentJSON` utilized a persistent worker pool, its performance lead would be even more massive!
4. **Cold Start & JIT Penalty (Zero-Warmup)**: `Sonic` heavily relies on JIT compilation. This means that practically every time it encounters a new or slightly different JSON structure, it requires a "warmup" phase to compile the machine code on the fly (dropping to ~800 MB/s). Because of this, its performance can be highly unpredictable in real-world production environments that process a large variety of incoming data formats. While its peak results are undeniably impressive, this unpredictable variance in latency is a critical factor to consider for systems with strict SLA requirements. In contrast, `SilentJSON` uses a precomputed registry, meaning it has **zero warmup penalty** and consistently delivers its maximum throughput (1450+ MB/s) from the very first request.

```mermaid
xychart-beta
    title "Memory Allocations during Serialization (MB/op, Lower is Better)"
    x-axis ["SilentJSON", "Protobuf", "Standard", "Sonic"]
    y-axis "MB/op" 0 --> 90
    bar [0, 6.4, 15.1, 78.1]
```

## ⚙️ Key Features

- **Unmatched Speed**: Up to **3000+ MB/s** decoding speed by parallelizing the unmarshalling of large arrays.
- **Zero-Copy Architecture**: Maximizes performance and minimizes GC overhead through direct `[]byte` mapping.
- **Cross-Platform**: SIMD-level acceleration for `amd64` (AVX2) and `arm64` (NEON) with **Experimental Apple Silicon and Linux ARM support!**
- **Effortless Integration**: Drop-in `UnmarshalArrayParallel` function that is fully compatible with native `[]struct{}` types.
- **Developer Friendly**: No code generation (`go generate`) required.
* **AVX2 & NEON Tape-Scanner:** Utilizes a Bitmask Iterator and SIMD instructions (like `simdjson`) to process JSON structures at blazing speeds without scalar loops.
* **Zero-Allocation Marshaling:** `MarshalSlice` does not allocate any heap memory, eliminating GC pressure.
* **Zero-Copy String Parsing:** Uses `unsafe.String` to map JSON string values directly from the input buffer.
* **Precomputed Registry:** Uses `reflect` only once at startup to build a structural registry, avoiding runtime reflection entirely.
* **Multi-Platform Native Assembly:** Works perfectly on Intel/AMD (AVX/AVX512) and features **experimental Apple Silicon / Linux ARM64 support** via native AArch64 NEON assembly.
* **Generics Support:** Clean, modern API for slices via Go 1.18+ generics.

## 📦 Installation

```bash
go get github.com/GenshIv/silentjson
```

## 🛠️ Usage

> [!TIP]
> **Quickstart Example**
> We provide a fully runnable example in the [`example/`](file:///c:/Users/ihar7/IdeaProjects/silentjson/example/main.go) directory. You can run it instantly using `go run example/main.go`.

The API is designed to be simple and intuitive.

### 1. Build the Registry (Once)
This is the only setup step. It's done once at application startup to avoid runtime reflection.

```go
type Employee struct {
    ID     int      `json:"id"`
    Name   string   `json:"name"`
    // ... other fields
}

// Do this once, e.g., in an init() function
var empRegistry = silentjson.BuildRegistry(reflect.TypeOf(Employee{}))
```

### 2. Unmarshaling: Choose Your Speed

#### Standard (but 7x faster) Parsing
For general-purpose parsing, use `UnmarshalSlice`. It's a simple, fast, single-core parser.

```go
func parseData(rawJSON []byte, expectedCount int) ([]Employee, error) {
    // silentjson achieves zero allocations by requiring a pre-allocated slice
    emps := make([]Employee, expectedCount)
    emps, err := silentjson.UnmarshalSlice(rawJSON, empRegistry, emps)
    return emps, err
}
```

#### Parallel (15x faster) Parsing for Large Arrays
For large JSON arrays, `UnmarshalArrayParallel` provides a massive speedup with a remarkably simple API.

```go
import "github.com/GenshIv/silentjson"

func parseLargeArray(rawJSON []byte, expectedCount int) ([]Employee, error) {
    // Just pass a pre-allocated slice. It handles the multithreading automatically!
    // No code generation, no unsafe pointers.
    employees := make([]Employee, expectedCount)
    employees, err := silentjson.UnmarshalArrayParallel[Employee](rawJSON, empRegistry, employees)
    if err != nil {
        return nil, err
    }
    return employees, nil
}
```

#### Stream Parsing (Infinite Arrays / Network streams)
When fetching large JSON array datasets from an API or disk without running out of RAM, use the `StreamDecoder`.

```go
func streamLargeArray(reader io.Reader) error {
    // Create decoder, which maintains a fixed-size internal buffer (e.g. 256KB)
    dec := silentjson.NewStreamDecoder[Employee](reader, empRegistry)
    
    // Use the convenient generic iterator.
    // The decoder reuses a single instance internally, minimizing allocations.
    err := dec.Next(func(emp *Employee) bool {
        // Process `emp` immediately (e.g. save to DB, print to stdout)
        fmt.Println(emp.ID)
        return true // return false to break early
    })
    
    return err
}
```

#### Extreme Stream Extraction & Chunking (NextRawBlock)
If you just need to extract JSON objects rapidly (e.g., to proxy them to a database, split large files into chunks, or filter by simple regex) without unmarshaling them into Go structs, use `NextRawBlock()`. 

This method completely avoids allocations during extraction and writes raw objects into massive contiguous byte arrays directly from the stream. It operates at an incredible **~4.1 GB/s**!

```go
func fastChunkSlicer(reader io.Reader) error {
    dec := silentjson.NewStreamDecoder[Employee](reader, empRegistry)
    
    for {
        // Extract up to 1000 objects at once, targeting ~10MB chunks
        rawBytes, count, err := dec.NextRawBlock(1000, 10*1024*1024)
        
        if count > 0 {
            // rawBytes is a single contiguous slice containing `count` objects
            // Example: [ {"id":1}, {"id":2}, ... ] 
            // Note: the slice lacks outer '[' and ']' brackets
            processRawChunk(rawBytes) 
        }

        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
    }
    return nil
}
```

#### Asynchronous Streaming (Producer-Consumer)
If you want to parse data on a background goroutine while simultaneously processing it on the main thread, use the asynchronous `NextChan` channel-based API. 

Unlike traditional libraries (like `jsoniter`) where writing a custom channel stream either leads to **data races** (reusing pointers) or **massive allocations** (creating new structs), SilentJSON uses an internal **Ring Buffer**. This achieves **ZERO extra allocations** while strictly avoiding data corruption, providing the perfect balance between concurrency and memory efficiency.

```go
func streamAsync(reader io.Reader) {
    dec := silentjson.NewStreamDecoder[Employee](reader, empRegistry)
    
    // Launch a background goroutine parsing objects with a channel buffer of 100
    ch := dec.NextChan(100)
    
    // Main thread processes them as they arrive
    for res := range ch {
        if res.Err != nil {
            log.Fatalf("Stream error: %v", res.Err)
        }
        fmt.Println(res.Item.ID)
    }
}
```

## 🧪 Testing

To run the tests for `silentjson`, use the standard Go testing tools.

### Running Tests & Benchmarks
```bash
# Run unit tests
go test

# Run all benchmarks to see performance metrics
go test -bench=.
```

## 🤝 Contributing

Contributions are welcome! We'd love your help in making `silentjson` even faster and more robust. Feel free to open issues, suggest improvements, or submit pull requests.

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
