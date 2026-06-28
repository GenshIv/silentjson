package main

import (
	"fmt"
)
import "golang.org/x/sys/cpu"

var hasAVX2 = cpu.X86.HasAVX2

func findQuote(data []byte) int {
	// Просто вызываем ассемблерную реализацию и возвращаем ее результат.
	return findQuoteAsm(data)
}

func main() {
	jsonData1 := []byte(`{"key": "value"}`)
	jsonData2 := []byte(`hello world`)

	index1 := findQuote(jsonData1)
	fmt.Printf("JSON data 1: %s\n", string(jsonData1))
	fmt.Printf("First quote found at index: %d\n\n", index1) // Ожидаем: 1

	index2 := findQuote(jsonData2)
	fmt.Printf("JSON data 2: %s\n", string(jsonData2))
	fmt.Printf("First quote found at index: %d\n", index2) // Ожидаем: -1

	written, read := parseShortStringASM(jsonData1[2:])
	if read > 0 {
		fmt.Printf("Parsed string scan: written=%d read=%d\n", len(written), read)
	}
}
