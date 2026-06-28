package main

import (
	"bytes"
	"fmt"
	"reflect"

	"github.com/GenshIv/silentjson"
)

type Employee struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	IsActive bool   `json:"isActive"`
}

func main() {
	fmt.Println("🚀 Running silentjson quickstart example...")

	// 1. Define the JSON payload
	rawJSON := []byte(`[
		{"id": 1, "name": "John Doe", "isActive": true},
		{"id": 2, "name": "Jane Smith", "isActive": false}
	]`)

	// 2. Build the registry ONCE at startup (avoids runtime reflection)
	registry := silentjson.BuildRegistry(reflect.TypeOf(Employee{}))

	// 3. Parse the data
	// silentjson achieves zero allocations by requiring you to provide a pre-allocated slice
	employees := make([]Employee, 2)
	employees, err := silentjson.UnmarshalArrayParallel[Employee](rawJSON, registry, employees)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// 4. Output the results
	fmt.Printf("✅ Successfully parsed %d employees using Parallel Marshal:\n", len(employees))
	for _, emp := range employees {
		fmt.Printf("   - [%d] %s (Active: %t)\n", emp.ID, emp.Name, emp.IsActive)
	}

	fmt.Println("\n🚀 Running Stream Decoder with Zero-Allocation Next() iterator...")
	
	// 5. Stream Parsing Example
	// We use the same JSON payload but read it as a stream.
	streamReader := bytes.NewReader(rawJSON)
	decoder := silentjson.NewStreamDecoder[Employee](streamReader, registry)
	
	fmt.Println("✅ Successfully streamed employees:")
	err = decoder.Next(func(emp *Employee) bool {
		// Inside this callback, `emp` points to a single reusable struct inside the decoder.
		// This yields zero-allocation streaming!
		fmt.Printf("   - [%d] %s (Active: %t)\n", emp.ID, emp.Name, emp.IsActive)
		return true // continue iteration
	})
	
	if err != nil {
		fmt.Printf("Stream Error: %v\n", err)
	}
}
