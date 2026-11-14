# silentjson: Высокопроизводительный JSON-парсер для Go
**Автор: Игорь Иванюто**

`silentjson` — это высокооптимизированная библиотека JSON для Go, не использующая рефлексию и аллокации, которая обеспечивает экстремальную производительность **без необходимости генерации кода.**

## 🚀 Почему `silentjson`?

- **Быстрый параллельный парсинг:** Для больших массивов JSON `UnmarshalArrayParallel` задействует все ядра процессора: ~3.0 ГБ/с со связыванием в структуры и ~1.1k аллокаций на 100k записей (Ryzen 9 7950X3D).
- **Без генерации кода:** В отличие от других быстрых библиотек, вам не нужен `go generate`. Все работает «из коробки».
- **Zero-Copy архитектура:** Строки отображаются напрямую из входного буфера, что минимизирует нагрузку на сборщик мусора (GC).

## 📊 Производительность (AMD Ryzen 9 7950X3D)

### 100k объектов, «хаос»-payload (обновлено в ноябре 2026)
| Парсер | Пропускная способность | Память | Аллокации |
| :--- | :--- | :--- | :--- |
| Goccy | 576 МБ/с | 665 МБ/op | 3.5M op |
| **SilentJSON (Serial)** | **486 МБ/с** ⭐ | **13 МБ/op** | **96 op** |
| Jsoniter | 333 МБ/с | 2045 МБ/op | 28.4M op |
| Buger Jsonparser | 169 МБ/с | 512 МБ/op | 26.4M op |
| Sonic | 147 МБ/с | 2350 МБ/op | 11.0M op |
| Standard (Go) | 138 МБ/с | 107 МБ/op | 5.1M op |

SilentJSON обеспечивает 2.8x ускорение по сравнению с Sonic и 3.0x по сравнению с encoding/json, при этом используя меньше памяти, чем все остальные парсеры. Оптимизация skipJSONString устраняет 2M временных аллокаций строк на итерацию для пропуска значений неизвестных полей без их декодирования.

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
