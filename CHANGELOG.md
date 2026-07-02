# Changelog

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
