package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/campusos/CampusOS/internal/plugin"
)

type contract struct {
	Version string                        `json:"version"`
	Items   []plugin.CapabilityDescriptor `json:"items"`
}

func main() {
	write := flag.Bool("write", false, "write the generated contract")
	path := flag.String("path", "docs/api/plugin-capabilities-v1.json", "contract path")
	flag.Parse()
	if err := plugin.ValidateCapabilityCatalog(); err != nil {
		fatal(err)
	}
	data, err := json.MarshalIndent(contract{Version: plugin.CapabilityCatalogVersion, Items: plugin.CapabilityCatalog()}, "", "  ")
	if err != nil {
		fatal(err)
	}
	data = append(data, '\n')
	if *write {
		if err := os.MkdirAll(filepath.Dir(*path), 0o755); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*path, data, 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("wrote %s\n", *path)
		return
	}
	current, err := os.ReadFile(*path)
	if err != nil {
		fatal(err)
	}
	if !bytes.Equal(current, data) {
		fatal(fmt.Errorf("contract drift: %s (run go run ./cmd/campusos-capability-contract --write)", *path))
	}
	fmt.Printf("ok %s\n", *path)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
