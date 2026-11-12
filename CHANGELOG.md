# Changelog

## [Unreleased] — empty destination panic fix + JSON spec conformance tests

### Fixed — UnmarshalSlice and UnmarshalSliceTolerant panicked on empty destination
Taking `&dst[0]` on an empty slice panicked before the capacity check could run. Both functions now defer basePtr computation until after the first window is scanned; if dst is empty but the array has elements, a clear `"insufficient capacity: need N, have 0"` error is returned instead of a panic. Empty arrays with empty destinations succeed as expected.

### Fixed — UnmarshalArrayParallel silently ignored non-empty arrays with empty destination
When dst was empty, the parallel API returned `nil, nil` without checking whether the array actually contained elements. It now scans for at least one element first; if elements exist but dst is empty, it returns `"insufficient capacity: array has elements but destination is empty"`.

### Added — comprehensive JSON spec conformance tests
New `json_spec_errors_test.go` covers 27 error cases across object syntax (missing braces, trailing commas, unquoted keys), array syntax, string escapes, number validation (leading zeros, multiple decimals), literal parsing (misspelled true/false/null), type mismatches, stream truncation behavior, and valid edge cases per RFC 8259.

## [2.1.1] - 2026-11-01 — audit fixes: envelope epilogue + float boundary + stream tail

Four post-release findings from the differential audit of 2.1.0 — all of
the "silently wrong" class, all introduced by 2.1.0 itself, all in code
paths the release advertised as proven (the differential fuzzer / the
bit-exact float fast path / the audited stream tail validation / the
strict number grammar).

### Fixed — the stream silently accepted a truncated envelope
A stream cut off before the closing bracket ended with a clean io.EOF,
making a truncated feed indistinguishable from a complete one:
`[{"id":1},{"id":2}` delivered both records and reported success (pinned
as "valid" until now), a bare `[` was reported as an empty array,
`[{"id":1},` (trailing comma at EOF) reported success — while
UnmarshalSlice rejects the same inputs (since P0-A), encoding/json reports
"unexpected end of JSON input", and even this library's own README
promised that "truncation is reported". The stream now reports
ErrUnexpectedEOF for every shape of a truncated envelope — WITHOUT losing
data. The healthy records are delivered first, including the final
element that is complete but lost its terminator (the splitter only cuts
at a comma or a bracket): nextChunk hands it out as a record and reports
the truncation on the NEXT call (io.Reader-style two-step, sticky on
repeated reads); NextRawBlock now salvages that final element the same
way (previously it dropped the valid record and errored — inconsistent
with nextChunk); the NextChanParallel worker parses a salvaged
terminator-less batch as the single value it is (re-splitting it finds
zero chunks, which silently dropped the record). Tolerant streams report
the defect through the sink exactly once (parity with
UnmarshalSliceTolerant, which already reported the unterminated array)
and end cleanly. Fixed on the way: the NextChanParallel worker read
d.isEOF directly while the producer was still filling the buffer (a data
race the race detector can now reach); the batch carries a dispatch-time
snapshot instead. Pinned by TestStreamTailUnterminatedReported (6 envelope
shapes x 5 streaming APIs x whole-buffer and socket-like reads), the
updated TestStreamTailComplete / TestStreamTailTruncated (sticky error,
mid-value fragment+error unchanged) and the tolerant parity case in
TestTolerantStream. After the fix: full suite green, -race green, both
differential fuzzers clean (90s/452k and 120s/610k execs).

### Fixed — UnmarshalSlice accepted a bare "[" as an empty array
The windowed rewrite added an early return for the zero-records case that
bypassed `arrayTailClosed`: `UnmarshalSlice("[")` returned 0 records and a
nil error, while `"[ "` was rejected (and while `encoding/json` rejects
both) — the outcome depended on a loop-entry accident. The tail epilogue
now runs on every path: with no scanned windows `prevEndAbs == 0`, so the
same check covers the whole inner region. `UnmarshalArrayParallel` and
`UnmarshalSliceTolerant` were already correct (the tolerant API reports
the unterminated array as a Problem on every such input). The
differential fuzzer finds the old behavior in ~2s; its crasher is in the
seed corpus (`da8972bff0a45585`). Pinned by
`TestFixEnvelopeTruncationRejected` (strict reject, parallel reject,
tolerant report, complete envelopes still accepted). After the fix:
120s differential fuzz round clean (622k execs).

### Fixed — parseFloatFast double-rounded at the 2^53 mantissa boundary
The pre-accumulation guard `mant > (1<<53)/10` let exactly one extra digit
through at the boundary (`mant == 900719925474099`), accumulating
mantissas in [9007199254740990, 9007199254740999] — the first values past
2^53, where `float64(mant)` is inexact and the single multiply/divide
double-rounds up to 1 ULP away from strconv: `"9007199254740995e-1"`
parsed as ...996 instead of ...995; also `"900719925474099.5"`, the
negated forms and the `e-22` variant. The randomized bit-exact test could
never hit this (a ~10-value window at 9e15; ~2e-10 expected hits in 200k
samples). The check now happens after accumulation
(`mant > 1<<53` → refuse): `mant == 2^53` stays on the fast path (exact),
anything past it falls back to strconv. Measured: no serial throughput
change (Medium within run-to-run noise on the same machine). Pinned by
`TestFastFloatMantissaBoundary` (exhaustive over the boundary window ×
8 exponent shapes, plus direct fast-path refusal assertions) and new
edge seeds.

### Fixed — a number truncated at EOF was reported as a type mismatch
A float token cut off by the end of the input fell out of parseFloatFast
with the same not-ok used for real garbage, so `{"price":12.`, `1.5e+` or
a bare `-` in a float field surfaced as ErrTypeMismatch "invalid number" —
the wrong error class: the grammar did not break, the input ended.
encoding/json classifies exactly these shapes at the buffer end as
"unexpected end of JSON input" (and keeps them invalid-character errors
when a delimiter follows). parseFloatFast now distinguishes truncation at
its three end-of-buffer sites and returns the value accumulated so far
alongside ("12." → 12, "1.5e+" → 1.5 — the fraction scale already carried
in exp10 applies); parseJSONFloat extends the classification to any
rejected token that is a proper prefix of a valid number (the new
incompleteNumber helper — this also covers the past-2^53 mantissa the
fast path refuses before it can see the cut, where no exact partial value
exists and the field is left at 0). The float decoders keep that partial
value in the field — allocating a fresh *float64 when needed — before
returning ErrUnexpectedEOF, so everything decoded before the cut survives
(the stream-tail contract: data first, then the sticky error);
skipValueStrict classifies unknown-field tokens the same way. A truncated
shape followed by a delimiter stays a type mismatch, untouched. Pinned by
TestFloatTruncatedAtEOFReportsEOFAndKeepsPartial (6 EOF shapes with
partial-value assertions), TestFloatTruncatedAtEOFPreservesEarlierFields,
TestFloatTruncatedAtEOFPointerPartial,
TestTruncatedNumberUnknownFieldAtEOF,
TestTruncatedShapeWithDelimiterStaysTypeMismatch (6 negative controls)
and TestParseJSONFloatTruncationClassification (unit table: 9 truncated /
7 complete / 12 garbage shapes). After the fix: full suite green, -race
green, both differential fuzzers clean (45s/217k and 45s/224k execs).

### Docs — corrected stale small-record claims
The "untouchable, 5x the nearest reflection parser" sentence and the
0.46 ns/byte figure for Small records were computed from the
truncation-inflated 2169 MB/s (a 31%-truncated decode, see 2.1.0);
corrected to the honest figures. Parallel small-record leadership
remains (3555 MB/s — 3.9x the fastest reflection parser).

## [2.1.0] - 2026-11-01 — correctness, tolerant mode, honest benchmarks

This release turns the library from "fast but silently corrupting on
edge cases" into "fast and provably careful":

- **Memory safety**: float32 field corruption, nil-marshaler SEGVs,
  GC-unsafe slice headers and checkptr violations are fixed (audit P0).
- **The SIMD chunk splitters are repaired**: the AVX2 splitter lost
  whole runs of records on escape sequences at 32-byte block
  boundaries (a strictly positional bug); the scalar splitter started
  every chunk at the comma. Both are fixed, pinned by exhaustive
  boundary sweeps and a byte-exact differential against a reference
  scanner — and the previously published multi-GB/s parallel numbers
  turned out to be measuring a broken error path.
- **Grammar parity with encoding/json** where it matters, plus a
  differential FUZZER that found five more holes (swallowed garbage,
  missing null handling, unchecked skipped values, phantom chunks,
  truncated arrays) — all fixed.
- **Tolerant mode** (design correction): a damaged record must not
  discard the healthy ones — process what you can, report Problems,
  let the caller decide. Strict APIs remain the fail-fast policy over
  the same machinery.
- **Honest, correctness-gated benchmarks** across parser x payload
  shape x flavor matrices, and performance that improved while getting
  correct: stream Decode 424→568 MB/s (past jsoniter), serial +11%,
  huge-record serial 9.1 GB/s, marshaling +11.5%, tolerant mode free
  on clean data.

### Fixed — silent truncation on arrays larger than the chunk-buffer estimate
The splitter stops when its chunk buffer fills (the documented "early exit" contract), but the serial `UnmarshalSlice`/`UnmarshalSliceTolerant` performed a SINGLE scan — arrays with more elements than the buffer estimate (len/128+1024) silently returned only the first window's records. 500k tiny records against a ~157k-chunk estimate parsed 31% of the payload, and the published "small records" serial numbers were ~3x overstated because of it. Both engines now loop over scan windows until the whole array is consumed (mirroring the parallel producer), the separator grammar is validated window-relative, and `TestSplitterWindowLoop` (200k records > estimate) pins full-count decoding. The array-tail check is what surfaced this: it turned the silent truncation into a loud error, which is exactly its job.

### Migration notes
- **Removed broken exported API**: `MarshalIntPtr` and `MarshalFloatPtr`
  (both corrupted memory for their documented types). Use the
  width-aware marshalers generated by `BuildRegistry` instead.
- **Strict parsing by default**: inputs that used to silently decode
  into garbage (`-42`→42, `3.9` into int, trailing commas, truncated
  arrays, `[{,}]`) now fail fast. If your feed is partially damaged,
  switch to the tolerant APIs added in this release.
- **`Marshal` panics** on registries with unsupported field kinds
  (interface{}, map, uint/uint64, chan, func) instead of emitting
  garbage; call `Registry.Err()` once at startup.
- **ARM64**: the NEON chunk splitters are routed to the (correct)
  scalar implementations pending the escape-carry audit; run
  `SILENTJSON_AUDIT_NEON=1 go test -run TestSplitter` on arm64
  hardware to execute that audit and re-enable them.

### Fixed (P0 — memory corruption / crashes, from the audit patch)
- **float32 fields are now read and written at their real 4-byte width** (P0-1). Previously every `float32` (and `*float32`) field was treated as `float64`: an 8-byte write corrupted the adjacent struct field and marshaling produced garbage. `FieldInfo` now carries `Bits`/`Unsigned`; parse, marshal and `omitempty` all honor the width, including pointer fields.
- **Unsupported field types fail loudly at registry build time** (P0-2). Fields of kind `interface{}`, `uint`/`uint64`, `uintptr`, map, chan, func, complex, array no longer silently become `TypeInt` with a nil `Marshaler` (nil-deref panic on Marshal, int bits written into the interface word on parse). `BuildRegistry` records the error (`Registry.Err()`), `Parse*` returns it, `Marshal` panics with a descriptive message. `int8/16/32`, `uint8/16/32` and `*int32`/`*float32`/etc. are now fully supported instead.
- **GC-safe slice growth**: `reflect.Value.Pointer()` (uintptr) results are no longer stored into slice headers/element slots; typed `Set`/`UnsafePointer` keep the backing arrays alive.

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

### Fixed — five grammar holes found by the differential fuzzer
Within minutes of turning the fuzzer on, before it was even committed:
- **Garbage after a field value was silently swallowed** (a legacy `skipValue` accepted `{"str":"a\\\\"end"}` as valid).
- **`null` was rejected** for string, struct, string-slice and int-slice fields and for the root value (encoding/json leaves the zero value).
- **Unknown-field and RawMessage values were skipped without grammar validation** (`n`, `tru`, `nullx`, `1.2.3` all passed); `skipValueStrict` now validates exact literals, strict numbers, paired escapes and balanced composites.
- **A zero chunk count with a stray comma** produced a phantom empty chunk and was reported as an empty array (`[,]` accepted).
- **A truncated array without the closing bracket** returned the parsed prefix as success — the slice APIs now require the array tail.

### Fixed — empty array with inner whitespace and separator grammar
- The AVX2 splitter initialized its chunk start at the buffer beginning instead of the first non-whitespace byte, so `UnmarshalSlice("[ ]")` failed ("insufficient capacity: need 1" / "expected object start"). Fixed with an entry whitespace-skip loop in assembly.
- Array element separators were never grammar-checked: leading (`[,{}]`), doubled (`[{},,{}]`) and trailing (`[{},]`) commas were accepted. `validArrayGap` now verifies the gaps between real elements in the serial, parallel and streaming paths.

### Fixed — streaming tails and separators
- A stream cut in the middle of a value used to hand the broken fragment out as a valid record (`NextRaw` returned `{"id` with a nil error). EOF fragments are now grammatically validated: exactly one complete value is delivered as the final record (a valid last element cut off from its terminator), anything else is a clean truncation error.
- On truncation, `NextRaw` returns the partial fragment TOGETHER with the error, io.Reader-style (n > 0, err != nil) — the tail bytes stay available to consumers that need them (e.g. resume after reconnect).
- Streaming paths skipped any comma they saw, so `[,{}]`, `[{},,{}]` and `[{},]` were accepted silently (and NextRawBlock even counted phantom chunks as records). Separators are now validated: leading/doubled/trailing commas are rejected in strict mode and reported in tolerant mode.

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
- **Serialization is ~11.5% faster than the pre-audit tree** (measured sequentially on both trees: 1159 → 1292 MB/s on the 100k-object generation benchmark, still zero-alloc). The gain comes from the audit patch itself: the `defer recover()` in `MarshalString` ran on every string-field marshal call and was removed — panics now propagate instead of being swallowed into truncated JSON.
- `Registry.Err() error` — preflight check for registries containing unsupported field types.
- `fixes_test.go` — regression tests covering every fix above; `fuzz_differential_test.go` — the differential fuzz targets with a seed corpus built from every real bug of this release; `splitter_regression_test.go`, `stream_tail_regression_test.go`, `float_fast_test.go` — boundary sweeps and bit-exactness pins.
- `RawMessage` gained `UnmarshalJSON`/`MarshalJSON` (json.RawMessage parity, raw copy in/out under encoding/json too).
- CI: `go test -race ./...` job in `.github/workflows/test.yml`.

### Added (profiling-driven optimizations: stream +34%, serial +11%)
- **Stream keys are no longer materialized**: keys are transient (hash lookup, case fallback, error text) and are now always zero-copy aliases regardless of the decoder's copyStrings policy, which exists for stored *values*. Stream Decode allocations drop from ~12.5 to ~3.5 per record (37.5M → 10.5M per 500 MB, memory 263 MB → 84 MB) and throughput rises 424 → 568 MB/s — past jsoniter's 454.
- **Single-pass fast float parser** (`parseFloatFast`): common number shapes (≤16 significant digits, |decimal exponent| ≤ 22) are validated and converted in one walk using the exactness of IEEE arithmetic (mant < 2^53, exactly representable 10^k, k ≤ 22: one multiply/divide IS the correctly rounded result); everything else falls back to validJSONNumber+strconv. Bit-identical to strconv on the edge cases and 200k randomized values; the differential fuzzer stays green.
- **Separator validation folded into the phantom-chunk filter** in the serial path (one pass over the chunks instead of two).
- Net effect measured: chaos serial 562 → 621 MB/s (+11%), medium serial 640 → 656, stream Decode 424 → 568 MB/s (+34% cumulative); parallel paths unchanged within their ±7% run noise.

### Changed (benchmarks)
- Benchmark hygiene: payload generation, registries, destination buffers and reusable parser state are strictly outside the timed region; defensive per-iteration `copy(buf, raw)` removed (the parser guarantees read-only input); every benchmark loop now checks errors (`b.Fatal`) — a benchmark must measure real work, not a fast error path.
- New `BenchmarkPayloadFlavors` (clean / escaped / utf8 variants of one payload, with a round-trip correctness test) and `BenchmarkParserComparison`: identical ~16 MB chaos payload (rotating key order, unknown fields, escaped strings, nulls) decoded by silentjson (serial + parallel), easyjson, simdjson-go, sonic, goccy, jsoniter and encoding/json, with `TestParserComparisonCorrectness` verifying every decoder against encoding/json field-by-field (it caught three API-misuse bugs in the simdjson extraction during development).
- New `benchpayload` package: comparison types with easyjson codegen (`-no_std_marshalers`, so other parsers keep their own code paths). Regenerate with `go run github.com/mailru/easyjson/easyjson@latest -no_std_marshalers benchpayload/payload.go`.
- New deps: `github.com/minio/simdjson-go v0.4.5`, `github.com/mailru/easyjson v0.9.2`.
- CI: go-version aligned with go.mod, `-race` job added.

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

### Known remaining limitations
- Untagged struct fields are ignored (silentjson is opt-in via `json` tags by design).
- `time.Time` and other tag-less structs marshal as `{}` and parse as zero values; use `RawMessage` or a custom representation.
- The NEON (arm64) chunk splitters are disabled pending the escape-carry audit (see Migration notes); the scalar splitters are used instead.
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
