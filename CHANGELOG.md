# Changelog

## [Unreleased] — audit patch + splitter correctness fixes + benchmark overhaul

### Fixed (P0 — chunk splitters corrupted arrays with escaped content)
- **Scalar array splitter off-by-one**: every chunk after the first started at the separating comma instead of the `{`, so `UnmarshalSlice`, `UnmarshalArrayParallel` and the stream decoder failed with "expected object start" on the second record on any non-AVX2 machine (and every ARM64 build). Hidden until now because all benchmark loops discarded errors.
- **Scalar splitter ignored the chunk-buffer limit**: it kept counting elements far beyond `len(chunks)` (violating the documented "stops when it fills chunks" contract), so callers such as `NextRawBlock` read stale zero chunks and returned empty blocks.
- **AVX2 assembly splitter mis-tracked escapes**: a backslash escape immediately followed by an escaped quote (any `json.Marshal`-ed string containing `\` + `"`, e.g. Windows paths) desynchronized the string scanner; runs of records were merged into single chunks or **silently dropped** (repro: 100k marshaled records lost ~3000, in contiguous runs like 71–90). The AVX2 splitter is now disabled in the dispatcher — the fixed scalar splitter is always used — until the assembly is fixed. This is the honest cost of correctness: parallel parsing runs at ~1.4 GB/s (scalar scan is the serial producer) instead of the previously published numbers, **which were measured on a fast-failing or silently-lossy error path** (benchmarks discarded errors).
- `splitter_regression_test.go` pins a `json.Marshal` round trip full of backslash-quote escapes through every array API, plus chunk-boundary invariants.
- **checkptr/vet**: element pointers were derived by arithmetic from raw `uintptr` header fields — crashed under `-race` (checkptr) and is the pattern vet flags. The backing-array start is now converted to `unsafe.Pointer` before element arithmetic; hand-rolled anonymous header structs in registry.go replaced with `reflect.SliceHeader`. `go vet` is clean outside the `.s` files; full suite passes with `-race`.

### Changed (benchmarks)
- Benchmark hygiene: payload generation, registries, destination buffers and reusable parser state are strictly outside the timed region; defensive per-iteration `copy(buf, raw)` removed (the parser guarantees read-only input); every benchmark loop now checks errors (`b.Fatal`) — a benchmark must measure real work, not a fast error path.
- New `BenchmarkParserComparison`: identical ~16 MB chaos payload (rotating key order, unknown fields, escaped strings, nulls) decoded by silentjson (serial + parallel), easyjson, simdjson-go, sonic, goccy, jsoniter and encoding/json, with `TestParserComparisonCorrectness` verifying every decoder against encoding/json field-by-field (it caught three API-misuse bugs in the simdjson extraction during development).
- New `benchpayload` package: comparison types with easyjson codegen (`-no_std_marshalers`, so other parsers keep their own code paths). Regenerate with `go run github.com/mailru/easyjson/easyjson@latest -no_std_marshalers benchpayload/payload.go`.
- New deps: `github.com/minio/simdjson-go v0.4.5`, `github.com/mailru/easyjson v0.9.2`.
- CI: go-version aligned with go.mod, `-race` job added.

### Fixed (P0 — memory corruption / crashes, from the audit patch)
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
- `findArrayElementsEarlyExitAVX2` / `findObjectBoundariesEarlyExitAVX2` are **disabled** (dispatcher routes to the scalar versions): their escape handling corrupts chunk boundaries (see the splitter fix above). Restoring them with correct escape pairing is the main follow-up for recovering parallel throughput.
- Parser laxness remaining vs `encoding/json`: raw control characters (< 0x20) inside string literals are accepted (encoding/json rejects them), and invalid UTF-8 bytes in strings pass through unchanged (encoding/json replaces them with U+FFFD).
- Marshalling a type with unsupported field kinds (interface{}, map, uint/uint64, chan, func) panics with a descriptive message; check `Registry.Err()` at startup.

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
