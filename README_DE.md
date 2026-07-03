# silentjson: Hochleistungs-JSON-Parser für Go

**Autor: Igor Ivanuto**

`silentjson` ist eine hochoptimierte, reflexionsfreie und allokationsfreie JSON-Bibliothek für Go, die extreme Performance bietet, **ohne dass eine Codegenerierung erforderlich ist.**

## 🚀 Warum `silentjson`?

- **Bis zu 30x schneller:** Bei großen JSON-Arrays nutzt `UnmarshalArrayParallel` alle CPU-Kerne und erreicht Geschwindigkeiten von über 12 GB/s.
- **Keine Codegenerierung:** Im Gegensatz zu anderen Bibliotheken ist kein `go generate` erforderlich. Es funktioniert sofort.
- **Zero-Copy-Architektur:** Strings werden direkt aus dem Eingabepuffer gemappt, was den Druck auf den Garbage Collector (GC) minimiert.

## 📊 Performance (AMD Ryzen 9 7950X3D)

### Architekturvergleich (100k Objekte)
| Modus | Durchsatz (MB/s) |
| :--- | :--- |
| **SilentJSON (AVX2)** | **24.670 MB/s** ⭐ |
| **SilentJSON (Skalar)** | **810 MB/s** |
| **Sonic (JIT)** | 644 MB/s |
| **Standard (Go)** | 110 MB/s |

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