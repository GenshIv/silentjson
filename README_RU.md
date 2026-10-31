# silentjson: Высокопроизводительный JSON-парсер для Go
**Автор: Игорь Иванюто**

`silentjson` — это высокооптимизированная библиотека JSON для Go, не использующая рефлексию и аллокации, которая обеспечивает экстремальную производительность **без необходимости генерации кода.**

## 🚀 Почему `silentjson`?

- **Быстрый параллельный парсинг:** Для больших массивов JSON `UnmarshalArrayParallel` задействует все ядра процессора: ~1.3 ГБ/с со связыванием в структуры и ~1.1k аллокаций на 100k записей (Ryzen 9 7950X3D).
- **Без генерации кода:** В отличие от других быстрых библиотек, вам не нужен `go generate`. Все работает «из коробки».
- **Zero-Copy архитектура:** Строки отображаются напрямую из входного буфера, что минимизирует нагрузку на сборщик мусора (GC).

## 📊 Производительность (AMD Ryzen 9 7950X3D)

### 100k объектов, «хаос»-payload (переснято в мае 2026)
| Парсер | Пропускная способность |
| :--- | :--- |
| **SilentJSON (Parallel)** | **1 258 МБ/с** ⭐ |
| Goccy | 680 МБ/с |
| **SilentJSON (Serial)** | **342 МБ/с** |
| Standard (Go) | 205 МБ/с |

## ⚙️ Ключевые особенности
- **SIMD ускорение:** Использует AVX2 на `amd64` для сверхбыстрой обработки.
- **Поддержка ARM64:** Экспериментальная поддержка для Apple Silicon и Linux ARM.
- **Shared Memory (SHM):** Идеально для IPC с низкой задержкой (Zero-Copy).
- **Стриминг:** Эффективное декодирование огромных потоков через `io.Reader`.

## 📦 Установка
```bash
go get github.com/GenshIv/silentjson
```

## 🛠️ Быстрый старт

### 1. Создание реестра (один раз)
```go
var empRegistry = silentjson.BuildRegistry(reflect.TypeOf(Employee{}))
```

### 2. Параллельная десериализация
```go
employees := make([]Employee, count)
employees, err := silentjson.UnmarshalArrayParallel[Employee](rawJSON, empRegistry, employees)
```

### 3. IPC через разделяемую память (SHM)
```go
// Декодирование напрямую из сегмента SHM без аллокаций в куче
err := silentjson.ParseObject(shmPayload, reg, unsafe.Pointer(&trade))
```

## 📰 Медиа и Сообщество
* **[Habr]** [silentjson v2.0.0: Уперлись в железо, или как мы выжали максимум из парсинга JSON в Go](https://habr.com/ru/news/1055022/)
* **[Reddit]** [silentjson v2.0.0: Hitting the hardware limits, or how we squeezed the maximum out of JSON parsing in Go](https://www.reddit.com/r/HiLoad/comments/1ulspzc/silentjson_v200_hitting_the_hardware_limits_or/)

## 📄 Лицензия
Лицензия MIT. Подробности в файле [LICENSE](LICENSE).
