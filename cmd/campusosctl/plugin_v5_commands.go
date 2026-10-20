package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/campusos/CampusOS/internal/plugin"
)

func runPlugin(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return 2
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "plugin init|validate|inspect|pack|sign|verify|catalog: offline v5 package tools; install, approve, configure and enable through the host Web/Admin")
		return 0
	}
	if err := runPluginV5(args, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runPluginV5(args []string, out io.Writer) error {
	if args[0] == "catalog" {
		if len(args) != 1 {
			return plugin.ErrPluginV5ManifestInvalid
		}
		host, ext := plugin.CurrentPluginV5Catalogs()
		return json.NewEncoder(out).Encode(map[string]json.RawMessage{"host": host, "extensions": ext})
	}
	if args[0] == "init" {
		return initPluginV5(args[1:], out)
	}
	if args[0] != "validate" && args[0] != "inspect" && args[0] != "pack" && args[0] != "sign" && args[0] != "verify" {
		return plugin.ErrPluginV5LegacyProtocol
	}
	fs := flag.NewFlagSet("plugin "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := fs.String("out", "", "new archive or detached envelope path; never overwritten")
	keyID := fs.String("key-id", "", "publisher signing key ID")
	keyFile := fs.String("key-file", "", "operator-selected base64 Ed25519 private key file")
	release := fs.String("release", "", "detached release envelope JSON")
	trust := fs.String("trust", "", "operator-selected publisher trust store JSON")
	_ = fs.Bool("json", false, "JSON output (always enabled)")
	target := ""
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		target, rest = rest[0], rest[1:]
	}
	if fs.Parse(rest) != nil {
		return plugin.ErrPluginV5ManifestInvalid
	}
	if target == "" && fs.NArg() == 1 {
		target = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return plugin.ErrPluginV5ManifestInvalid
	}
	if target == "" {
		return plugin.ErrPluginV5ManifestInvalid
	}
	var raw []byte
	var p *plugin.PluginV5Package
	var err error
	info, statErr := os.Stat(target)
	if statErr == nil && info.IsDir() {
		if args[0] == "sign" || args[0] == "verify" {
			return plugin.ErrPluginV5PackageInvalid
		}
		raw, p, err = plugin.BuildPluginV5Package(target)
	} else {
		raw, err = plugin.ReadPluginV5PackageFile(target)
		if err == nil {
			p, err = plugin.InspectPluginV5Package(raw)
		}
	}
	if err != nil {
		return err
	}
	switch args[0] {
	case "pack":
		if info == nil || !info.IsDir() || *output == "" {
			return plugin.ErrPluginV5PackageInvalid
		}
		if err = writeV5Output(*output, raw); err != nil {
			return err
		}
	case "sign":
		if *output == "" || *keyFile == "" || *keyID == "" {
			return plugin.ErrPluginV5PackageSignature
		}
		encoded, err := readV5SmallFile(*keyFile)
		if err != nil {
			return plugin.ErrPluginV5PackageSignature
		}
		private, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(encoded)))
		if err != nil || len(private) != ed25519.PrivateKeySize {
			return plugin.ErrPluginV5PackageSignature
		}
		envelope, err := plugin.SignPluginV5Package(raw, *keyID, ed25519.PrivateKey(private))
		clear(private)
		clear(encoded)
		if err != nil {
			return err
		}
		if err = writeV5Output(*output, envelope); err != nil {
			return err
		}
	case "verify":
		envelope, err := readV5SmallFile(*release)
		if err != nil {
			return plugin.ErrPluginV5PackageSignature
		}
		trustBytes, err := readV5SmallFile(*trust)
		if err != nil {
			return plugin.ErrPluginV5PackageSignature
		}
		p, err = plugin.VerifyPluginV5Release(raw, envelope, trustBytes)
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(out).Encode(map[string]any{"api_version": p.Manifest.APIVersion, "publisher": p.Manifest.Publisher, "key": p.Manifest.Key, "version": p.Manifest.Version, "runtime": p.Manifest.Runtime, "package_digest": p.Digest, "file_count": p.FileCount, "signature_verified": args[0] == "verify", "installed": false})
}

func readV5SmallFile(path string) ([]byte, error) {
	raw, err := plugin.ReadPluginV5DocumentFile(path)
	if err != nil || len(raw) > plugin.PluginV5ManifestMaxBytes {
		return nil, plugin.ErrPluginV5ManifestInvalid
	}
	return raw, nil
}
func v5CLIBytesDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func writeV5Output(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("plugin.output_exists_or_unavailable")
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err = f.Write(raw); err != nil {
		return plugin.ErrPluginV5PackageInvalid
	}
	if err = f.Sync(); err != nil {
		return plugin.ErrPluginV5PackageInvalid
	}
	if err = f.Close(); err != nil {
		return plugin.ErrPluginV5PackageInvalid
	}
	ok = true
	return nil
}

func initPluginV5(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("plugin init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	directory := fs.String("dir", "", "new source directory")
	runtime := fs.String("runtime", "none", "v5 source template: none")
	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if fs.Parse(args) != nil || fs.NArg() != 0 || *runtime != "none" {
		return plugin.ErrPluginV5LegacyProtocol
	}
	if *directory == "" {
		*directory = name
	}
	if plugin.ValidatePluginName(name) != nil || strings.ContainsAny(name, "._") || len(name) < 2 {
		return plugin.ErrPluginV5ManifestInvalid
	}
	// A minimal UI source template has no consumed capability, backend, secret,
	// installation hook or executable build script.
	ui := []byte("<!doctype html><html><head><title>Plugin</title></head><body>v5 UI source template</body></html>\n")
	digest := v5CLIBytesDigest(ui)
	manifest := map[string]any{"api_version": "campusos.plugin/v5", "publisher": "local", "key": name, "version": "0.1.0", "host_contracts": []string{"campusos.host/v1"}, "runtime": "none", "ui": []any{map[string]any{"audience": "user", "entrypoint": "dist/user/index.html", "digest": digest}}, "provides": []any{}, "consumes": []any{}, "configuration": map[string]any{"contract": "campusos.plugin-config/v1", "version": "v1", "schema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}, "required": []any{}}, "defaults": map[string]any{}, "selectors": []any{}, "secret_names": []any{}}, "data": map[string]any{"config_scope": "both", "private_kv_bytes": 0, "private_object_bytes": 0, "upgrade": map[string]any{"schema_version": 1, "mode": "preserve", "rollback": "snapshot"}}}
	wire, _ := json.MarshalIndent(manifest, "", "  ")
	if _, err := plugin.ParsePluginV5Manifest(wire); err != nil {
		return err
	}
	if err := os.Mkdir(*directory, 0700); err != nil {
		return errors.New("plugin.output_exists_or_unavailable")
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(*directory)
		}
	}()
	if os.MkdirAll(filepath.Join(*directory, "dist/user"), 0700) != nil || os.WriteFile(filepath.Join(*directory, "dist/user/index.html"), ui, 0600) != nil || os.WriteFile(filepath.Join(*directory, plugin.PluginV5ManifestFile), append(wire, '\n'), 0600) != nil {
		return plugin.ErrPluginV5PackageInvalid
	}
	ok = true
	fmt.Fprintln(out, "created plugin scaffold (v5 UI-only; no installation or execution)")
	return nil
}
