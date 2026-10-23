package main

import (
	"context"
	"github.com/campusos/CampusOS/internal/plugin/wasm"
	"os"
)

func main() {
	if len(os.Args) != 1 || wasm.RunV5Worker(context.Background(), os.Stdin, os.Stdout) != nil {
		os.Exit(1)
	}
}
