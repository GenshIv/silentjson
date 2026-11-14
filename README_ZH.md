# silentjson: 适用于 Go 的高性能 JSON 解析器

**作者: Igor Ivanuto**

`silentjson` 是一个高度优化的、无反射、零分配的 Go 语言 JSON 库，在**无需任何代码生成**的情况下提供极速性能。

## 🚀 为什么选择 `silentjson`?

- **快速并行解析：** 大型 JSON 数组 `UnmarshalArrayParallel` 利用所有 CPU 核心：结构绑定约 3.0 GB/s（Ryzen 9 7950X3D）。
- **无需代码生成：** 与其他高性能库不同，不需要 `go generate`。即插即用，无开发摩擦。
- **零拷贝 (Zero-Copy) 架构：** 字符串直接从输入缓冲区映射，极大降低了垃圾回收 (GC) 压力。

## 📊 性能表现 (AMD Ryzen 9 7950X3D)

### 10万个对象，"混沌"负载（2026 年 11 月更新）
| 解析器 | 吞吐量 (MB/s) | 内存 | 分配次数 |
| :--- | :--- | :--- | :--- |
| Goccy | 576 MB/s | 665 MB/op | 3.5M op |
| **SilentJSON (Serial)** | **486 MB/s** ⭐ | **13 MB/op** | **96 op** |
| Jsoniter | 333 MB/s | 2045 MB/op | 28.4M op |
| Buger Jsonparser | 169 MB/s | 512 MB/op | 26.4M op |
| Sonic | 147 MB/s | 2350 MB/op | 11.0M op |
| 标准库 (Go) | 138 MB/s | 107 MB/op | 5.1M op |

SilentJSON 比 Sonic 快 2.8 倍，比 encoding/json 快 3.0 倍，同时使用的内存比其他所有解析器都少。skipJSONString 优化通过在不解码的情况下跳过未知字段值，每次迭代消除 2M 次临时字符串分配。

## ⚙️ 核心特性
- **SIMD 加速：** 在 `amd64` 上使用 AVX2 指令集实现超高速处理。
- **ARM64 支持：** 针对 Apple Silicon 和 Linux ARM 的实验性支持。
- **共享内存 (SHM)：** 结合共享内存实现低延迟的零拷贝进程间通信 (IPC)。
- **流式处理：** 通过 `io.Reader` 高效解析海量数据流。

## 📦 安装
```bash
go get github.com/GenshIv/silentjson
```

## 🛠️ 快速上手

### 1. 构建注册表（仅需一次）
```go
var empRegistry = silentjson.BuildRegistry(reflect.TypeOf(Employee{}))
```

### 2. 并行反序列化
```go
employees := make([]Employee, count)
employees, err := silentjson.UnmarshalArrayParallel[Employee](rawJSON, empRegistry, employees)
```

### 3. 通过共享内存 (SHM) 进行 IPC
```go
// 直接从 SHM 段解码，无需堆分配
err := silentjson.ParseObject(shmPayload, reg, unsafe.Pointer(&trade))
```

## 📄 许可证
本项目采用 MIT 许可证。详见 [LICENSE](LICENSE) 文件。