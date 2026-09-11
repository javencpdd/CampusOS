package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
)

type commandResult struct {
	Command  string   `json:"command"`
	Plugin   string   `json:"plugin"`
	Runtime  string   `json:"runtime"`
	Status   string   `json:"status"`
	Steps    []string `json:"steps,omitempty"`
	Artifact string   `json:"artifact,omitempty"`
}

func runPluginBuild(args []string, stdout io.Writer) error {
	dir, jsonOutput, err := parsePluginDirCommand("plugin build", args)
	if err != nil {
		return err
	}
	manifest, artifact, steps, err := buildPlugin(dir)
	if err != nil {
		return err
	}
	return writeCommandResult(stdout, jsonOutput, commandResult{Command: "build", Plugin: manifest.Name, Runtime: manifest.Runtime, Status: "pass", Steps: steps, Artifact: artifact})
}

func runPluginTest(args []string, stdout io.Writer) error {
	dir, jsonOutput, err := parsePluginDirCommand("plugin test", args)
	if err != nil {
		return err
	}
	manifest, steps, err := testPlugin(dir)
	if err != nil {
		return err
	}
	return writeCommandResult(stdout, jsonOutput, commandResult{Command: "test", Plugin: manifest.Name, Runtime: manifest.Runtime, Status: "pass", Steps: steps})
}

func runPluginVerify(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "emit machine-readable JSON")
	target := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if target == "" && fs.NArg() == 1 {
		target = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return errors.New("usage: campusosctl plugin verify <plugin-dir|package.tar.gz> [--json]")
	}
	if target == "" {
		return errors.New("usage: campusosctl plugin verify <plugin-dir|package.tar.gz> [--json]")
	}
	manifest, err := verifyPlugin(target)
	if err != nil {
		return err
	}
	return writeCommandResult(stdout, *jsonOutput, commandResult{Command: "verify", Plugin: manifest.Name, Runtime: manifest.Runtime, Status: "pass", Steps: []string{"manifest", "permissions", "runtime-artifact", "package-safety"}})
}

func runPluginDev(args []string, stdout io.Writer) error {
	dir, jsonOutput, err := parsePluginDirCommand("plugin dev", args)
	if err != nil {
		return err
	}
	manifest, testSteps, err := testPlugin(dir)
	if err != nil {
		return fmt.Errorf("test step: %w", err)
	}
	_, artifact, buildSteps, err := buildPlugin(dir)
	if err != nil {
		return fmt.Errorf("build step: %w", err)
	}
	if _, err := verifyPlugin(dir); err != nil {
		return fmt.Errorf("verify step: %w", err)
	}
	steps := append(testSteps, buildSteps...)
	steps = append(steps, "verify")
	return writeCommandResult(stdout, jsonOutput, commandResult{Command: "dev", Plugin: manifest.Name, Runtime: manifest.Runtime, Status: "pass", Steps: steps, Artifact: artifact})
}

func runPluginDoctor(args []string, stdout io.Writer) error {
	dir, jsonOutput, err := parsePluginDirCommand("plugin doctor", args)
	if err != nil {
		return err
	}
	manifest, err := plugin.ValidatePluginPackageDir(dir)
	if err != nil {
		return err
	}
	if err := plugin.ValidateCapabilityCatalog(); err != nil {
		return err
	}
	steps := []string{"manifest", "capability-catalog", "configuration-schema", "runtime-contract"}
	if manifest.Runtime == "grpc" {
		steps = append(steps, "warning: grpc is a compatibility alias; migrate to process")
	}
	return writeCommandResult(stdout, jsonOutput, commandResult{Command: "doctor", Plugin: manifest.Name, Runtime: manifest.Runtime, Status: "pass", Steps: steps})
}

func runPluginConformance(args []string, stdout io.Writer) error {
	dir, jsonOutput, err := parsePluginDirCommand("plugin conformance", args)
	if err != nil {
		return err
	}
	manifest, testSteps, err := testPlugin(dir)
	if err != nil {
		return fmt.Errorf("runtime tests: %w", err)
	}
	_, artifact, buildSteps, err := buildPlugin(dir)
	if err != nil {
		return fmt.Errorf("runtime build: %w", err)
	}
	if _, err := verifyPlugin(dir); err != nil {
		return fmt.Errorf("package contract: %w", err)
	}
	if err := plugin.ValidateCapabilityCatalog(); err != nil {
		return fmt.Errorf("capability contract: %w", err)
	}
	steps := append(testSteps, buildSteps...)
	steps = append(steps, "manifest-v3", "capability-catalog", "unknown-operation-default-deny", "package-safety")
	return writeCommandResult(stdout, jsonOutput, commandResult{Command: "conformance", Plugin: manifest.Name, Runtime: manifest.Runtime, Status: "pass", Steps: steps, Artifact: artifact})
}

func runPluginWatch(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin watch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	once := fs.Bool("once", false, "run one development cycle and exit")
	interval := fs.Duration("interval", time.Second, "poll interval")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return errors.New("usage: campusosctl plugin watch <plugin-dir> [--once] [--interval 1s]")
	}
	dir := fs.Arg(0)
	runCycle := func() error { return runPluginDev([]string{dir}, stdout) }
	if err := runCycle(); err != nil || *once {
		return err
	}
	last, err := pluginTreeStamp(dir)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			stamp, err := pluginTreeStamp(dir)
			if err != nil {
				return err
			}
			if stamp != last {
				last = stamp
				if err := runCycle(); err != nil {
					fmt.Fprintf(stdout, "watch cycle failed: %v\n", err)
				}
			}
		}
	}
}

func pluginTreeStamp(dir string) (string, error) {
	latest := int64(0)
	count := int64(0)
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") && path != dir {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		count++
		if value := info.ModTime().UnixNano(); value > latest {
			latest = value
		}
		return nil
	})
	return fmt.Sprintf("%d:%d", latest, count), err
}

func parsePluginDirCommand(name string, args []string) (string, bool, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "emit machine-readable JSON")
	dir := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", false, err
	}
	if dir == "" && fs.NArg() == 1 {
		dir = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return "", false, fmt.Errorf("usage: campusosctl %s <plugin-dir> [--json]", name)
	}
	if dir == "" {
		return "", false, fmt.Errorf("usage: campusosctl %s <plugin-dir> [--json]", name)
	}
	return dir, *jsonOutput, nil
}

func buildPlugin(dir string) (*plugin.Manifest, string, []string, error) {
	manifest, err := plugin.ValidatePluginPackageDir(dir)
	if err != nil {
		return nil, "", nil, err
	}
	switch manifest.Runtime {
	case "grpc", "process":
		if err := runCommand(dir, nil, "go", "build", "-o", "plugin", "."); err != nil {
			return nil, "", nil, err
		}
		artifact, err := grpcArtifactPath(dir)
		if err != nil {
			return nil, "", nil, err
		}
		return manifest, artifact, []string{"go build"}, nil
	case "wasm":
		artifact := filepath.Join(dir, "plugin.wasm")
		if _, err := os.Stat(artifact); err != nil {
			return nil, "", nil, fmt.Errorf("wasm artifact missing: %s; compile source with a WASI-compatible toolchain", artifact)
		}
		if err := validateWasmMagic(artifact); err != nil {
			return nil, "", nil, err
		}
		return manifest, artifact, []string{"wasm header validated"}, nil
	default:
		return nil, "", nil, fmt.Errorf("unsupported runtime %q", manifest.Runtime)
	}
}

func testPlugin(dir string) (*plugin.Manifest, []string, error) {
	manifest, err := plugin.ValidatePluginPackageDir(dir)
	if err != nil {
		return nil, nil, err
	}
	if manifest.Runtime == "wasm" {
		artifact := filepath.Join(dir, "plugin.wasm")
		if err := validateWasmMagic(artifact); err != nil {
			return nil, nil, err
		}
		return manifest, []string{"wasm smoke validation"}, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		if err := runCommand(dir, nil, "go", "test", "./...", "-count=1"); err != nil {
			return nil, nil, err
		}
		return manifest, []string{"go test ./..."}, nil
	}
	return manifest, []string{"manifest-only test"}, nil
}

func verifyPlugin(target string) (*plugin.Manifest, error) {
	var manifest *plugin.Manifest
	var err error
	info, statErr := os.Stat(target)
	if statErr == nil && info.IsDir() {
		manifest, err = plugin.ValidatePluginPackageDir(target)
	} else {
		manifest, err = plugin.InspectPluginPackage(target)
	}
	if err != nil {
		return nil, err
	}
	for _, permission := range manifest.Permissions.API {
		for _, action := range permission.Actions {
			if !plugin.IsKnownPermission(permission.Resource, action) {
				return nil, fmt.Errorf("manifest permission is not in the Host API permission catalog: %s/%s", permission.Resource, action)
			}
		}
	}
	if statErr == nil && info.IsDir() {
		switch manifest.Runtime {
		case "grpc", "process":
			artifact, err := grpcArtifactPath(target)
			if err != nil {
				return nil, fmt.Errorf("grpc runtime requires executable artifact %s", artifact)
			}
		case "wasm":
			if err := validateWasmMagic(filepath.Join(target, "plugin.wasm")); err != nil {
				return nil, err
			}
		}
	}
	return manifest, nil
}

// grpcArtifactPath accepts the normal Unix artifact and the Windows .exe
// variant. Windows has no meaningful POSIX execute bit, so a regular local
// file with the expected name is the executable contract there.
func grpcArtifactPath(dir string) (string, error) {
	candidates := []string{filepath.Join(dir, "plugin")}
	if runtime.GOOS == "windows" {
		candidates = []string{filepath.Join(dir, "plugin.exe"), filepath.Join(dir, "plugin")}
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return candidates[0], errors.New("artifact missing or is not executable")
}

func validateWasmMagic(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read wasm artifact: %w", err)
	}
	if len(data) < 8 || !bytes.Equal(data[:4], []byte{0x00, 0x61, 0x73, 0x6d}) {
		return fmt.Errorf("invalid wasm artifact: %s", path)
	}
	return nil
}

func runCommand(dir string, env []string, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("%s timed out", name)
	}
	if err != nil {
		return fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeCommandResult(stdout io.Writer, asJSON bool, result commandResult) error {
	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	fmt.Fprintf(stdout, "%s %s: %s (%s)\n", result.Command, result.Plugin, result.Status, result.Runtime)
	for _, step := range result.Steps {
		fmt.Fprintf(stdout, "- %s\n", step)
	}
	if result.Artifact != "" {
		fmt.Fprintf(stdout, "artifact: %s\n", result.Artifact)
	}
	return nil
}
