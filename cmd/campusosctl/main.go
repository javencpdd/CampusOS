package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/campusos/CampusOS/internal/plugin"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "plugin":
		return runPlugin(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runPlugin(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printPluginUsage(stderr)
		return 2
	}
	switch args[0] {
	case "init":
		if err := runPluginInit(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "plugin init: %v\n", err)
			return 1
		}
		return 0
	case "inspect":
		if err := runPluginInspect(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "plugin inspect: %v\n", err)
			return 1
		}
		return 0
	case "pack":
		if err := runPluginPack(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "plugin pack: %v\n", err)
			return 1
		}
		return 0
	case "install":
		if err := runPluginInstall(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "plugin install: %v\n", err)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		printPluginUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown plugin command: %s\n", args[0])
		printPluginUsage(stderr)
		return 2
	}
}

func runPluginInit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	runtime := fs.String("runtime", "wasm", "plugin runtime: wasm or grpc")
	dir := fs.String("dir", "", "target directory; defaults to the plugin name")
	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if name == "" {
		if fs.NArg() != 1 {
			return errors.New("usage: campusosctl plugin init <name> [--runtime wasm|grpc] [--dir path]")
		}
		name = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return errors.New("usage: campusosctl plugin init <name> [--runtime wasm|grpc] [--dir path]")
	}
	if err := validatePluginName(name); err != nil {
		return err
	}
	if *runtime != "wasm" && *runtime != "grpc" {
		return fmt.Errorf("runtime must be wasm or grpc, got %q", *runtime)
	}
	targetDir := *dir
	if targetDir == "" {
		targetDir = name
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	manifestPath := filepath.Join(targetDir, "plugin.yaml")
	if _, err := os.Stat(manifestPath); err == nil {
		return fmt.Errorf("%s already exists", manifestPath)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := os.WriteFile(manifestPath, []byte(pluginManifestTemplate(name, *runtime)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(pluginReadmeTemplate(name)), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "created plugin scaffold: %s\n", targetDir)
	return nil
}

func runPluginInspect(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: campusosctl plugin inspect <plugin-dir>")
	}
	pluginDir := fs.Arg(0)
	manifest, err := plugin.LoadManifest(filepath.Join(pluginDir, "plugin.yaml"))
	if err != nil {
		return err
	}
	result := map[string]interface{}{
		"name":          manifest.Name,
		"display_name":  manifest.DisplayName,
		"version":       manifest.Version,
		"runtime":       manifest.Runtime,
		"events":        manifest.Events.Subscribe,
		"permissions":   manifest.Permissions.API,
		"storage":       manifest.Storage,
		"config":        manifest.Config,
		"config_schema": manifest.ConfigSchema,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func runPluginPack(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin pack", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "output package path")
	pluginDir := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pluginDir = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if pluginDir == "" {
		if fs.NArg() != 1 {
			return errors.New("usage: campusosctl plugin pack <plugin-dir> [--out path]")
		}
		pluginDir = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return errors.New("usage: campusosctl plugin pack <plugin-dir> [--out path]")
	}
	manifest, err := plugin.LoadManifest(filepath.Join(pluginDir, "plugin.yaml"))
	if err != nil {
		return err
	}
	if err := validatePluginPackageFiles(pluginDir, manifest); err != nil {
		return err
	}

	outputPath := *out
	if outputPath == "" {
		outputPath = filepath.Join(filepath.Dir(filepath.Clean(pluginDir)), fmt.Sprintf("%s-%s.campusos-plugin.tar.gz", manifest.Name, manifest.Version))
	}
	if err := createPluginArchive(pluginDir, outputPath); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "packed plugin: %s\n", outputPath)
	return nil
}

func runPluginInstall(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "examples/plugins", "target plugins directory")
	replace := fs.Bool("replace", false, "replace existing plugin directory")
	packagePath := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		packagePath = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if packagePath == "" {
		if fs.NArg() != 1 {
			return errors.New("usage: campusosctl plugin install <package.tar.gz> [--dir plugins-dir] [--replace]")
		}
		packagePath = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return errors.New("usage: campusosctl plugin install <package.tar.gz> [--dir plugins-dir] [--replace]")
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp(*dir, ".install-*")
	if err != nil {
		return err
	}
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.RemoveAll(tempDir)
		}
	}()

	if err := extractPluginArchive(packagePath, tempDir); err != nil {
		return err
	}
	manifest, err := plugin.LoadManifest(filepath.Join(tempDir, "plugin.yaml"))
	if err != nil {
		return err
	}
	if err := validatePluginName(manifest.Name); err != nil {
		return err
	}
	targetDir := filepath.Join(*dir, manifest.Name)
	if _, err := os.Stat(targetDir); err == nil {
		if !*replace {
			return fmt.Errorf("%s already exists; use --replace to overwrite", targetDir)
		}
		if err := os.RemoveAll(targetDir); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tempDir, targetDir); err != nil {
		return err
	}
	keepTemp = true
	fmt.Fprintf(stdout, "installed plugin: %s\n", targetDir)
	return nil
}

func validatePluginPackageFiles(pluginDir string, manifest *plugin.Manifest) error {
	if err := validatePluginName(manifest.Name); err != nil {
		return err
	}
	if manifest.Runtime == "wasm" {
		modulePath := "plugin.wasm"
		if raw, ok := manifest.Config["module"]; ok {
			if value, ok := raw.(string); ok && value != "" {
				modulePath = value
			}
		}
		if err := requireRelativePathInside(pluginDir, modulePath); err != nil {
			return fmt.Errorf("invalid wasm module path: %w", err)
		}
		if _, err := os.Stat(filepath.Join(pluginDir, modulePath)); err != nil {
			return fmt.Errorf("wasm module %s: %w", modulePath, err)
		}
	}
	return nil
}

func createPluginArchive(pluginDir, outputPath string) error {
	pluginRoot, err := filepath.Abs(filepath.Clean(pluginDir))
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(filepath.Clean(outputPath))
	if err != nil {
		return err
	}
	outputFile, err := os.Create(outputAbs)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	gz := gzip.NewWriter(outputFile)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.WalkDir(pluginRoot, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		fileAbs, err := filepath.Abs(filePath)
		if err != nil {
			return err
		}
		if fileAbs == outputAbs {
			return nil
		}
		rel, err := filepath.Rel(pluginRoot, fileAbs)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkipPluginPackagePath(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(fileAbs)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func extractPluginArchive(packagePath, targetDir string) error {
	file, err := os.Open(packagePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	targetRoot, err := filepath.Abs(filepath.Clean(targetDir))
	if err != nil {
		return err
	}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		cleanName, err := cleanArchiveName(header.Name)
		if err != nil {
			return err
		}
		if shouldSkipPluginPackagePath(cleanName, header.FileInfo().IsDir()) {
			return fmt.Errorf("archive contains forbidden path: %s", header.Name)
		}
		targetPath := filepath.Join(targetRoot, filepath.FromSlash(cleanName))
		targetAbs, err := filepath.Abs(filepath.Clean(targetPath))
		if err != nil {
			return err
		}
		if targetAbs != targetRoot && !strings.HasPrefix(targetAbs, targetRoot+string(os.PathSeparator)) {
			return fmt.Errorf("archive path escapes target: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetAbs, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(targetAbs, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry type for %s", header.Name)
		}
	}
}

func cleanArchiveName(name string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || clean == ".." || path.IsAbs(clean) {
		return "", fmt.Errorf("unsafe archive path: %s", name)
	}
	return clean, nil
}

func shouldSkipPluginPackagePath(rel string, isDir bool) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case ".git", "data", "node_modules":
		return true
	}
	if !isDir {
		base := filepath.Base(rel)
		if strings.HasSuffix(base, ".log") || strings.HasSuffix(base, ".tmp") {
			return true
		}
	}
	return false
}

func requireRelativePathInside(rootDir, relPath string) error {
	if relPath == "" || filepath.IsAbs(relPath) {
		return fmt.Errorf("path must be relative: %s", relPath)
	}
	rootAbs, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(filepath.Clean(filepath.Join(rootAbs, relPath)))
	if err != nil {
		return err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return fmt.Errorf("path escapes plugin directory: %s", relPath)
	}
	return nil
}

func validatePluginName(name string) error {
	if name == "" {
		return errors.New("plugin name is required")
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("plugin name %q can only contain lowercase letters, numbers, hyphen, and underscore", name)
	}
	return nil
}

func pluginManifestTemplate(name, runtime string) string {
	config := ""
	configSchema := ""
	if runtime == "wasm" {
		config = `  module: "plugin.wasm"
  entrypoint: "handle_event"
  event_timeout_ms: 1000`
		configSchema = `  fields:
    - key: "entrypoint"
      label: "Entrypoint"
      type: "string"
      required: true
      default: "handle_event"
    - key: "event_timeout_ms"
      label: "Event timeout"
      type: "number"
      required: true
      default: 1000`
	} else {
		config = `  command: "./plugin"
  event_timeout_ms: 1000`
		configSchema = `  fields:
    - key: "command"
      label: "Command"
      type: "string"
      required: true
      default: "./plugin"
    - key: "event_timeout_ms"
      label: "Event timeout"
      type: "number"
      required: true
      default: 1000`
	}
	return strings.TrimSpace(fmt.Sprintf(`
name: %s
display_name: "%s"
version: "0.1.0"
description: "CampusOS plugin"
author: "CampusOS Developer"
runtime: %s

events:
  subscribe:
    - "thread.created"

permissions:
  api:
    - resource: "config"
      actions: ["read"]

storage:
  type: none

config:
%s

config_schema:
%s
`, name, name, runtime, config, configSchema)) + "\n"
}

func pluginReadmeTemplate(name string) string {
	return strings.TrimSpace(fmt.Sprintf(`
# %s

CampusOS plugin scaffold generated by campusosctl.

## Inspect

`+"```bash"+`
go run ./cmd/campusosctl plugin inspect .
`+"```"+`
`, name)) + "\n"
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: campusosctl <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  plugin    plugin scaffolding and inspection")
}

func printPluginUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: campusosctl plugin <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  init      create a plugin scaffold")
	fmt.Fprintln(w, "  inspect   inspect a plugin manifest")
	fmt.Fprintln(w, "  pack      package a plugin directory")
	fmt.Fprintln(w, "  install   install a packaged plugin")
}
