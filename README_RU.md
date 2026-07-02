# silentjson: Высокопроизводительный JSON-парсер для Go

`silentjson` — это высокооптимизированная библиотека JSON для Go, не использующая рефлексию и аллокации, которая обеспечивает экстремальную производительность **без необходимости генерации кода.**

## 🚀 Почему `silentjson`?

- **До 30 раз быстрее:** Для больших массивов JSON `UnmarshalArrayParallel` задействует все ядра процессора, достигая скорости более 12 ГБ/с.
- **Без генерации кода:** В отличие от других быстрых библиотек, вам не нужен `go generate`. Все работает «из коробки».
- **Zero-Copy архитектура:** Строки отображаются напрямую из входного буфера, что минимизирует нагрузку на сборщик мусора (GC).

## 📊 Производительность (AMD Ryzen 9 7950X3D)

### Сравнение архитектур (100k объектов)
| Режим | Пропускная способность (МБ/с) |
| :--- | :--- |
| **SilentJSON (AVX2)** | **24 670 МБ/с** ⭐ |
| **SilentJSON (Scalar)** | **810 МБ/с** |
| **Sonic (JIT)** | 644 МБ/с |
| **Standard (Go)** | 110 МБ/с |

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
