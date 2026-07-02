package main

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"time"
	"unsafe"

	"github.com/GenshIv/hft-ipc/ringbuf"
	"github.com/GenshIv/hft-ipc/shm"
	"github.com/GenshIv/silentjson"
)

type Trade struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
	Qty    int     `json:"qty"`
}

func main() {
	filePath := "test_shm_zero_copy.bin"
	capacityBytes := uint64(1024 * 64) // 64 KB

	size := int(ringbuf.DataOffset) + int(capacityBytes)

	mapped, file, err := shm.OpenOrCreateMmap(filePath, size)
	if err != nil {
		log.Fatalf("Failed to mmap: %v", err)
	}
	defer file.Close()
	defer mapped.Unmap()
	defer os.Remove(filePath)

	rb := ringbuf.Init(mapped, capacityBytes)

	// Producer
	go func() {
		jsonChunks := []string{
			`{"symbol": "BTCUSDT", "price": 60500.5, "qty": 2}`,
			`{"symbol": "ETHUSDT", "price": 3100.0, "qty": 10}`,
			`{"symbol": "SOLUSDT", "price": 145.2, "qty": 100}`,
		}

		fmt.Println("[Producer] Запускаю отправку чанков в разделяемую память...")
		for _, chunk := range jsonChunks {
			for !rb.Push(mapped, []byte(chunk)) {
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Consumer
	fmt.Println("[Consumer] Ожидаю JSON чанки через mmap...")
	reg := silentjson.BuildRegistry(reflect.TypeOf(Trade{}))

	processed := 0
	for processed < 3 {
		if payload, ok := rb.GrabChunk(mapped); ok {
			var trade Trade

			err := silentjson.ParseObject(payload, reg, unsafe.Pointer(&trade))
			if err != nil {
				fmt.Printf("[Consumer] Ошибка парсинга: %v\n", err)
			} else {
				fmt.Printf("[Consumer] Успешно распарсили: Symbol=%s, Price=%.2f, Qty=%d\n", trade.Symbol, trade.Price, trade.Qty)
			}
			processed++
		}
	}

	fmt.Println("[Main] Интеграционный пример успешно завершен.")
}
