# Changelog

## [Unreleased] — audit patch

### Fixed (P0 — memory corruption / crashes)
- **float32 fields are now read and written at their real 4-byte width** (P0-1). Previously every `float32` (and `*float32`) field was treated as `float64`: an 8-byte write corrupted the adjacent struct field and marshaling produced garbage. `FieldInfo` now carries `Bits`/`Unsigned`; parse, marshal and `omitempty` all honor the width, including pointer fields.
- **Unsupported field types fail loudly at registry build time** (P0-2). Fields of kind `interface{}`, `uint`/`uint64`, `uintptr`, map, chan, func, complex, array no longer silently become `TypeInt` with a nil `Marshaler` (nil-deref panic on Marshal, int bits written into the interface word on parse). `BuildRegistry` records the error (`Registry.Err()`), `Parse*` returns it, `Marshal` panics with a descriptive message. `int8/16/32`, `uint8/16/32` and `*int32`/`*float32`/etc. are now fully supported instead.
- **GC-safe slice growth**: `reflect.Value.Pointer()` (uintptr) results are no longer stored into slice headers/element slots; typed `Set`/`UnsafePointer` keep the backing arrays alive.

### Fixed (P1 — correctness)
- **Strict JSON number grammar for integer fields** (P1-1): `-42` parses as `-42` (the sign was silently dropped), `3.9`, `1e2`, `true`, `007`, empty/garbage tokens and out-of-range values now return an error instead of garbage; `null` leaves the zero value (like `encoding/json`). Float fields validate the token and reject range errors. Negative values work in `[]int` arrays.
- **`\uXXXX` escapes are decoded, including surrogate pairs** (P1-2): `"\u0041\u0042"` → `"AB"`, `"\ud83d\ude00"` → `😀`; unpaired surrogates become U+FFFD; invalid escapes (`\q`, `\u00zz`) are errors, matching `encoding/json`.
- **The input buffer is never modified** (P1-3): escaped strings are decoded into fresh allocations instead of being compacted in place; unescaped strings remain zero-copy aliases (documented behavior). Consequences: RAW payloads stay byte-exact and the same buffer can be parsed by concurrent readers.
- **`NewStreamDecoder` no longer mutates the shared `Registry`** (P1-4): copy-on-read string semantics became per-decoder state, removing both the data race on `Registry.CopyStrings` and the global behavior change for direct `ParseObject` callers.
- **`BuildRegistry` is safe for concurrent use** (P1-4): registries are published to the cache only when fully built (double-checked locking + a building map anchors recursive types). Previously a partially built registry was published immediately, racing with concurrent readers.

### Fixed (P2 — strictness & compat)
- **Trailing data after a root array is rejected** (`[{"i":1}]GARBAGE`, `[{"i":1}] [{"i":2}]`), as are **trailing commas** (`{"i":1,}`, `{,}`, `[1,]`) — matching `encoding/json`.
- **`*Struct` fields can be parsed** (previously only marshaled): `{"p":{"x":9}}` allocates and fills the pointee, `null` sets nil.
- **`RawMessage` fields can be parsed**: the raw value is captured and copied (marshal side already existed).
- **Case-insensitive key fallback** (exact match still wins), like `encoding/json`.
- `null` is accepted for int/float/bool fields (leaves zero value) and for all pointer fields.

### Fixed (raw.go utilities)
- `GetStringValue`/`GetInt64Value`: top-level keys win over nested ones; keys are matched after unescaping, so string **values** can no longer produce false matches; nested lookup works as a fallback.
- `InjectFieldBeforeClose`/`InjectFieldBeforeClose1`: no panic on empty input, non-object input is returned unchanged, `{}` no longer produces `{,"k":"v"}` (leading comma omitted), keys are JSON-escaped when needed.

### Added
- `Registry.Err() error` — preflight check for registries containing unsupported field types.
- `fixes_test.go` — regression tests covering every fix above.
- CI: `go test -race ./...` job in `.github/workflows/test.yml`.

### Known remaining limitations (documented, not part of this patch)
- Untagged struct fields are ignored (silentjson is opt-in via `json` tags by design).
- `time.Time` and other tag-less structs marshal as `{}` and parse as zero values; use `RawMessage` or a custom representation.
- `findQuoteOrEscape` has no AVX2 implementation yet (the declared symbol was never implemented in the .s file); the scalar fallback is always used.

## [2.0.0] - 2026-07-02

### Changed
- **Major Version Update (v2)**: Updated module path to `github.com/GenshIv/silentjson/v2` to follow Go semantic versioning for major releases.
- **Improved API Stability**: Finalized core APIs for high-performance parsing and marshaling.

### Added
- **SHM Integration (Zero-copy IPC)**: Added support for parsing JSON directly from Shared Memory (mmap) without heap allocations. 
- **Zero-Allocation Marshaling**: Introduced `MarshalSlice` and `Marshal` functions for high-performance serialization with buffer reuse.
- **IPC Example**: Added a complete integration example with `hft-ipc` in `example/shm_integration/`.

### Fixed & Improved
- **Scalar Fallback Optimization**: Major algorithmic improvements to pure Go scalar parser, achieving up to 829 MB/s (outperforming Sonic's AVX2 JIT in some scenarios).
- **Architecture Stability**: Improved runtime detection and fallbacks for older CPU architectures and ARM64.
- **Benchmark Suite**: Added micro-payload benchmarks (5 objects) and large-scale generation benchmarks.
- **Documentation**: Updated READMEs in multiple languages (RU, ZH, FR, DE) with new benchmarks and feature descriptions.
