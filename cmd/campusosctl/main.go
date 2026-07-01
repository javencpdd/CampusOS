package main

import (
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
	if err := plugin.ValidatePluginName(name); err != nil {
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
	info, err := plugin.PackagePlugin(pluginDir, *out)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "packed plugin: %s\n", info.PackagePath)
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
	info, err := plugin.InstallPluginPackage(packagePath, *dir, *replace)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed plugin: %s\n", info.PluginDir)
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
