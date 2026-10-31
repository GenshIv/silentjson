# silentjson: Hochleistungs-JSON-Parser für Go

**Autor: Igor Ivanuto**

`silentjson` ist eine hochoptimierte, reflexionsfreie und allokationsfreie JSON-Bibliothek für Go, die extreme Performance bietet, **ohne dass eine Codegenerierung erforderlich ist.**

## 🚀 Warum `silentjson`?

- **Schnelles Parallel-Parsing:** Bei großen JSON-Arrays nutzt `UnmarshalArrayParallel` alle CPU-Kerne: ~3,0 GB/s mit Struct-Binding (Ryzen 9 7950X3D).
- **Keine Codegenerierung:** Im Gegensatz zu anderen Bibliotheken ist kein `go generate` erforderlich. Es funktioniert sofort.
- **Zero-Copy-Architektur:** Strings werden direkt aus dem Eingabepuffer gemappt, was den Druck auf den Garbage Collector (GC) minimiert.

## 📊 Performance (AMD Ryzen 9 7950X3D)

### 100k Objekte, "Chaos"-Payload (neu gemessen Mai 2026)
| Parser | Durchsatz (MB/s) |
| :--- | :--- |
| **SilentJSON (Parallel)** | **2.963 MB/s** ⭐ |
| Goccy | 671 MB/s |
| **SilentJSON (Serial)** | **532 MB/s** |
| Standard (Go) | 206 MB/s |

> Die alten Multi-GB/s-Parallelwerte waren ungültig (Benchmarks ignorierten Fehler; der AVX2-Splitter verlor Datensätze bei Escape-Sequenzen). Vollständige Tabellen in [README.md](README.md).

## ⚙️ Hauptmerkmale
- **SIMD-Beschleunigung:** Nutzt AVX2 auf `amd64` für ultraschnelle Verarbeitung.
- **ARM64-Unterstützung:** Experimentelle Unterstützung für Apple Silicon und Linux ARM.
- **Shared Memory (SHM):** Ideal für IPC mit geringer Latenz (Zero-Copy).
- **Streaming:** Effizientes Dekodieren massiver Datenströme über `io.Reader`.

## 📦 Installation
```bash
go get github.com/GenshIv/silentjson
```

## 🛠️ Schnelleinstieg

### 1. Registry erstellen (einmalig)
```go
var empRegistry = silentjson.BuildRegistry(reflect.TypeOf(Employee{}))
```

### 2. Parallele Deserialisierung
```go
employees := make([]Employee, count)
employees, err := silentjson.UnmarshalArrayParallel[Employee](rawJSON, empRegistry, employees)
```

### 3. IPC über Shared Memory (SHM)
```go
// Direktes Dekodieren aus einem SHM-Segment ohne Heap-Allokationen
err := silentjson.ParseObject(shmPayload, reg, unsafe.Pointer(&trade))
```

## 📄 Lizenz
Lizenziert unter der MIT-Lizenz. Siehe [LICENSE](LICENSE) für Details.