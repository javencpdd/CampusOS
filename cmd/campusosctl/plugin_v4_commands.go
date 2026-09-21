package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pluginv4 "github.com/campusos/CampusOS/internal/plugin/v4"
)

func runPluginV4(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: campusosctl plugin v4 <validate|stage|pack|sign|install> ...")
	}
	switch args[0] {
	case "validate":
		return runPluginV4Validate(args[1:], stdout)
	case "stage":
		return runPluginV4Stage(args[1:], stdout)
	case "pack":
		return runPluginV4Pack(args[1:], stdout)
	case "sign":
		return runPluginV4Sign(args[1:], stdout)
	case "install":
		return runPluginV4Install(args[1:], stdout)
	default:
		return fmt.Errorf("unknown v4 plugin command %q", args[0])
	}
}

func runPluginV4Validate(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: campusosctl plugin v4 validate <source-dir>")
	}
	manifest, err := pluginv4.ValidateSourceDirectory(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "valid v4 source: %s %s\n", manifest.Key, manifest.Version)
	return nil
}

func runPluginV4Stage(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin v4 stage", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "empty release output directory")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || strings.TrimSpace(*out) == "" {
		return errors.New("usage: campusosctl plugin v4 stage <source-dir> --out <empty-release-dir>")
	}
	manifest, err := pluginv4.PrepareReleaseDirectory(fs.Arg(0), *out)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "staged v4 release: %s %s -> %s\n", manifest.Key, manifest.Version, filepath.Clean(*out))
	return nil
}

func runPluginV4Pack(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin v4 pack", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "release archive path")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || strings.TrimSpace(*out) == "" {
		return errors.New("usage: campusosctl plugin v4 pack <source-dir> --out <release.tar.gz>")
	}
	stage, err := os.MkdirTemp("", "campusos-v4-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if _, err := pluginv4.PrepareReleaseDirectory(fs.Arg(0), stage); err != nil {
		return err
	}
	info, err := pluginv4.BuildReleaseArchive(stage, *out)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "packed v4 release: %s (%s)\n", info.ArchivePath, info.Digest)
	return nil
}

func runPluginV4Sign(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin v4 sign", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "signed release archive path")
	keyID := fs.String("key-id", "", "trusted Ed25519 key identifier")
	keyFile := fs.String("key-file", "", "base64-encoded Ed25519 private key file")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || *keyID == "" || *keyFile == "" {
		return errors.New("usage: campusosctl plugin v4 sign <release.tar.gz> --key-id <id> --key-file <base64-private-key> [--out <signed.tar.gz>]")
	}
	encoded, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	privateKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("v4 signing key file must contain one base64 Ed25519 private key")
	}
	info, err := pluginv4.SignReleaseArchive(fs.Arg(0), *out, *keyID, ed25519.PrivateKey(privateKey))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "signed v4 release: %s (%s)\n", info.ArchivePath, info.Digest)
	return nil
}

func runPluginV4Install(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin v4 install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", "plugins", "plugins root containing .installed")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return errors.New("usage: campusosctl plugin v4 install <release.tar.gz> [--root plugins]")
	}
	info, err := pluginv4.InstallReleaseArchive(fs.Arg(0), pluginv4.InstallOptions{RootDir: *root})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed v4 release: %s %s -> %s\n", info.Manifest.Key, info.Manifest.Version, info.InstallPath)
	return nil
}
