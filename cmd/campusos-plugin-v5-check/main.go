// campusos-plugin-v5-check validates declarations against explicit operator
// catalog snapshots. It cannot install, execute, authenticate or approve a package.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"

	"github.com/campusos/CampusOS/internal/plugin"
)

type result struct {
	Contract             string `json:"contract"`
	Valid                bool   `json:"manifest_contract_valid"`
	Code                 string `json:"code,omitempty"`
	Scope                string `json:"scope"`
	Installable          bool   `json:"installable"`
	PackageBytesVerified bool   `json:"package_bytes_verified"`
	SignatureVerified    bool   `json:"signature_verified"`
}

func run(args []string, out io.Writer) int {
	r := result{Contract: "campusos.plugin/v5", Scope: "declaration_and_explicit_catalog_snapshots"}
	finish := func(code string, status int) int {
		r.Code = code
		r.Valid = status == 0
		if json.NewEncoder(out).Encode(r) != nil {
			return 1
		}
		return status
	}
	flags := flag.NewFlagSet("campusos-plugin-v5-check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifest := flags.String("manifest", "", "complete v5 JSON manifest")
	host := flags.String("host-catalog", "", "explicit host capability snapshot JSON")
	ext := flags.String("extension-catalog", "", "explicit host extension snapshot JSON")
	release := flags.String("release", "", "release envelope JSON; signature is not verified")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *manifest == "" || *host == "" || *ext == "" || *release == "" {
		return finish("plugin.manifest_invalid", 2)
	}
	var inputs [][]byte
	for _, path := range []string{*manifest, *host, *ext, *release} {
		raw, ok := readInput(path)
		if !ok {
			return finish("plugin.manifest_invalid", 2)
		}
		inputs = append(inputs, raw)
	}
	if _, err := plugin.ValidatePluginV5Manifest(inputs[0], inputs[1], inputs[2], inputs[3]); err != nil {
		return finish(err.Error(), 1)
	}
	return finish("", 0)
}

func readInput(path string) ([]byte, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > plugin.PluginV5ManifestMaxBytes {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(f, plugin.PluginV5ManifestMaxBytes+1))
	return raw, err == nil && len(raw) <= plugin.PluginV5ManifestMaxBytes
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
