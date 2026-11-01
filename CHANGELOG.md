# Changelog

## [Unreleased] — audit patch + splitter correctness fixes + benchmark overhaul

### Fixed (P0 — AVX2 chunk splitter: escape-carry bug, now repaired and re-enabled)
- **Root cause found and fixed in assembly** (`find_quote_amd64.s`): when a backslash lands on the LAST byte of a 32-byte SIMD block, the scanner sets an "escape carry" for the next block — but the escaped byte at the start of the next block was removed only from the struct and quote masks, not from the backslash mask, so an escaped backslash ran the state machine again as a new escape introducer and skipped one byte too many. The scalar tail (<32 remaining bytes) ignored the carry entirely, letting an escaped quote toggle the in-string state. Either path flipped the string parity, and whole runs of records were then merged into single chunks or silently dropped (json.Marshal output of 100k records lost ~3000, in contiguous runs). The failure was strictly positional (escape at offset ≡ 31 mod 32), which is why small hand-written test cases never caught it.
- The fix: the carry now clears bit 0 of all three masks (struct, quote, backslash), and the scalar tail consumes the carry once before its loop (new `fae_enter_tail` / `foee_enter_tail` trampolines). Applied to both `findArrayElementsEarlyExitAVX2` and `findObjectBoundariesEarlyExitAVX2`.
- **The AVX2 splitters are re-enabled in the dispatcher.** Verified by new regression tests: a positional sweep of escape sequences across block boundaries (`TestSplitterEscapeCarryBoundary`) and a full differential against a byte-wise reference scanner on json.Marshal output (`TestSplitterMarshalDifferential` — 100000/100000 chunks, byte-exact boundaries). With AVX2 back: parallel parsing 1.35 GB/s → **3.0 GB/s**, stream NextRawBlock 1.28 GB/s → **4.0 GB/s**, Decode 229 → **386 MB/s**.
- `findQuoteOrEscapeScalar`: `bytes.IndexAny` replaced with two `bytes.IndexByte` scans — IndexAny built an ASCII set on every call (a top CPU consumer in post-audit parse profiles, since every key and string value passes through this function).

### Fixed (P0 — scalar chunk splitters)
- **Scalar array splitter off-by-one**: every chunk after the first started at the separating comma instead of the `{`, so `UnmarshalSlice`, `UnmarshalArrayParallel` and the stream decoder failed with "expected object start" on the second record on any non-AVX2 machine (and every ARM64 build). Hidden until now because all benchmark loops discarded errors.
- **Scalar splitter ignored the chunk-buffer limit**: it kept counting elements far beyond `len(chunks)` (violating the documented "stops when it fills chunks" contract), so callers such as `NextRawBlock` read stale zero chunks and returned empty blocks. Fixed with a proper early exit matching the AVX2 contract.
- `splitter_regression_test.go` pins a `json.Marshal` round trip full of backslash-quote escapes through every array API, plus chunk-boundary invariants.
- **checkptr/vet**: element pointers were derived by arithmetic from raw `uintptr` header fields — crashed under `-race` (checkptr) and is the pattern vet flags. The backing-array start is now converted to `unsafe.Pointer` before element arithmetic; hand-rolled anonymous header structs in registry.go replaced with `reflect.SliceHeader`. `go vet` is clean outside the `.s` files; full suite passes with `-race`.

### Performance regression analysis (measured, not guessed)
BenchmarkPayloadFlavors runs the same 100k-record payload as clean ASCII, with escapes (10% of strings), and with multibyte UTF-8 — on both the pre-audit tree (1a869d8) and the current one — so every claim below is a measurement:

| Serial UnmarshalSlice, MB/s | pre-audit (1a869d8) | current |
| :--- | :--- | :--- |
| clean (plain ASCII) | 622 | 537 (-14%) |
| escaped (10% of strings) | **fails** (chunk-boundary error) | 541 |
| utf8 (multibyte runes) | 650 | 560 (-14%) |

1. **Not unicode.** Multibyte UTF-8 costs nothing on either tree (the scanner looks for `"` and `\` bytes; UTF-16/UTF-8 work happens only on the `\uXXXX` branch of escaped strings). The utf8 flavor is the *fastest* row on both trees.
2. **Not escapes either — for throughput.** Escapes add allocations for decoded strings (~1.2 MB per 16 MB payload) but no measurable time; the pre-audit tree could not complete the escaped row at all (it failed on corrupted chunk boundaries).
3. **The real, uniform cost: ~14% on the serial parse path**, identical on clean and utf8 payloads — the strict number grammar (leading zeros, range checks, rejected garbage), width-aware field access and the readJSONString scanner replacing the old assembly short-string parser. This is the audit's correctness bill, not an escape/unicode penalty.
4. **Pre-audit multi-GB/s parallel numbers were never real** — they measured a fast-failing or silently-lossy error path (benchmarks discarded errors). With the repaired AVX2 splitter, correct parallel parsing runs at ~3.0 GB/s and the raw stream paths fully recover (NextRawBlock ~4.0 GB/s).
5. **Stream Decode (386 MB/s vs the old unsound 562)** — the remaining gap is the per-decoder string copying (the internal buffer is overwritten by the io.Reader, so zero-copy aliases would dangle), plus the same ~14% strict-parse cost.

### Changed (benchmarks)
- Benchmark hygiene: payload generation, registries, destination buffers and reusable parser state are strictly outside the timed region; defensive per-iteration `copy(buf, raw)` removed (the parser guarantees read-only input); every benchmark loop now checks errors (`b.Fatal`) — a benchmark must measure real work, not a fast error path.
- New `BenchmarkPayloadFlavors` (clean / escaped / utf8 variants of one payload, with a round-trip correctness test) and `BenchmarkParserComparison`: identical ~16 MB chaos payload (rotating key order, unknown fields, escaped strings, nulls) decoded by silentjson (serial + parallel), easyjson, simdjson-go, sonic, goccy, jsoniter and encoding/json, with `TestParserComparisonCorrectness` verifying every decoder against encoding/json field-by-field (it caught three API-misuse bugs in the simdjson extraction during development).
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
- **Tolerant mode: process what you can, report the rest** (design correction). The library's philosophy for real-world feeds is that a damaged record must not discard the healthy ones. `UnmarshalSliceTolerant(raw, reg, dst) ([]T, []Problem)` decodes every clean record, skips and reports damaged ones, validates/reports separator and termination defects, and salvages the valid tail element of an unterminated array. `NewStreamDecoder(r, reg).Tolerant(sink)` does the same for streams: a damaged record is reported through the sink and the stream keeps flowing; truncation and stray commas are reported, not fatal. The strict APIs are the fail-fast policy over the same machinery (first problem becomes the error) and keep their signatures and behavior. Measured: tolerant ≡ strict on clean data (same time, same 1 allocation); problems allocate only when they occur.
- **Serialization is ~11.5% faster than the pre-audit tree** (measured sequentially on both trees: 1159 → 1292 MB/s on the 100k-object generation benchmark, still zero-alloc). The gain comes from the audit patch itself: the `defer recover()` in `MarshalString` ran on every string-field marshal call and was removed — panics now propagate instead of being swallowed into truncated JSON. The AVX2 string scanner is not involved here (marshaling scans strings inside `appendStringAVX2`).
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
