# silentjson: The "Just Works" High-Performance JSON Parser for Go
![silentjson.jpg](assets/silentjson.jpg)
`silentjson` is a highly optimized, reflection-free, and zero-allocation JSON library for Go that delivers extreme performance **without requiring any code generation.**

## 🚀 Why `silentjson`?

In a world of high-performance Go libraries, `silentjson` stands out by providing massive speed boosts with zero developer friction.

- **Up to 15x Faster Parsing:** For large JSON arrays, `UnmarshalArrayParallel` leverages all your CPU cores, achieving a performance increase of over 1500% compared to the standard library.
- **7x Faster Standard Parsing:** Even on a single core, `UnmarshalSlice` is over 7 times faster than `encoding/json` for typical JSON objects.
- **Zero Code Generation:** This is the key. Unlike other fast JSON libraries, you don't need to generate any code. There are no extra build steps, no `go:generate` commands to remember, and no complex CI/CD pipeline configurations. **It works out-of-the-box, just like the standard library, only much faster.** This makes it trivial to integrate into any project, including those deployed in Docker or Kubernetes environments.

## Performance Deep Dive

Benchmarks were run on an **AMD Ryzen 9 7950X3D (16-core)** against a 482 MB payload.

| Operation | `silentjson` (Single-Core) | `silentjson` (Multi-Core) | `encoding/json` (Standard) |
| :--- | :--- | :--- | :--- |
| **Unmarshal** | ~748 MB/s | **~1740 MB/s** | ~106 MB/s |
| **Marshal** | **~722 MB/s** | (N/A) | ~599 MB/s |

### Benchmark Logs (AMD Ryzen 9 7950X3D)
```
goos: windows
goarch: amd64
pkg: github.com/GenshIv/silentjson
cpu: AMD Ryzen 9 7950X3D 16-Core Processor          
BenchmarkNestedStandard-32                     2        4785220200 ns/op         105.74 MB/s    162143144 B/op  13343122 allocs/op
BenchmarkNestedSystem-32                       8         676207612 ns/op         748.26 MB/s    13506576 B/op     324677 allocs/op
BenchmarkMarshalStandardSlice-32             219          26511254 ns/op         599.22 MB/s    16045902 B/op          2 allocs/op
BenchmarkMarshalSystemSlice-32               262          21991524 ns/op         722.37 MB/s           0 B/op          0 allocs/op
BenchmarkUnmarshalArrayParallel-32           625           9130701 ns/op        1739.84 MB/s       66842 B/op        486 allocs/op
```

## ⚙️ Key Features

* **No Code Generation:** Drop-in replacement that works immediately.
* **Automated Parallel Parsing:** `UnmarshalArrayParallel` automatically handles memory allocation and provides a simple, clean API for maximum throughput.
* **Zero-Allocation Marshaling:** `MarshalSlice` does not allocate any heap memory, eliminating GC pressure.
* **Zero-Copy String Parsing:** Uses `unsafe.String` to map JSON string values directly from the input buffer.
* **Precomputed Registry:** Uses `reflect` only once at startup to build a structural registry, avoiding runtime reflection entirely.
* **Generics Support:** Clean, modern API for slices via Go 1.18+ generics.

## 📦 Installation

```bash
go get github.com/GenshIv/silentjson
```

## 🛠️ Usage

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
func parseData(rawJSON []byte) ([]Employee, error) {
    var emps []Employee
    err := silentjson.UnmarshalSlice(rawJSON, empRegistry, unsafe.Pointer(&emps))
    return emps, err
}
```

#### Parallel (15x faster) Parsing for Large Arrays
For large JSON arrays, `UnmarshalArrayParallel` provides a massive speedup with a remarkably simple API.

```go
import "github.com/GenshIv/silentjson"

func parseLargeArray(rawJSON []byte) ([]Employee, error) {
    // Just call the function. It handles everything.
    // No code generation, no manual allocation, no unsafe pointers.
    employees, err := silentjson.UnmarshalArrayParallel[Employee](rawJSON, empRegistry)
    if err != nil {
        return nil, err
    }
    return employees, nil
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

## ⚠️ Caveats & Considerations

* **`unsafe` package:** This library heavily utilizes the `unsafe` package. Use with care.
* **Input Buffer Immutability:** Because strings are mapped directly via zero-copy, the underlying `rawJSON` byte slice **must not be modified** while the parsed objects are still in use.
* **Memory Retention (Zero-Copy Side Effect):** Because strings hold direct references to the original `rawJSON` buffer, retaining even a single parsed string in memory will prevent the entire underlying JSON byte array from being garbage collected. If you only need to store a small subset of parsed data for a long time, explicitly copy the strings (e.g., using `strings.Clone(val)`).
* **CPU Usage (Parallel Parsing):** `UnmarshalArrayParallel` is designed to use all available CPU cores to maximize speed for large payloads. It is ideal for batch processing or data pipelines. Avoid using it inside individual, high-concurrency API handlers, as this can lead to excessive goroutine creation. For per-request parsing, `UnmarshalSlice` is the better choice.
