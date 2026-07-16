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

	platformresource "github.com/campusos/CampusOS/internal/platform/resource"
)

func runResource(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printResourceUsage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "adopt":
		err = runResourceAdopt(args[1:], stdout)
	case "inspect":
		err = runResourceInspect(args[1:], stdout)
	case "help", "-h", "--help":
		printResourceUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown resource command: %s\n", args[0])
		printResourceUsage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "resource %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func runResourceAdopt(args []string, stdout io.Writer) error {
	directoryArgument := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		directoryArgument = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("resource adopt", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	kind := flags.String("type", "", "resource type")
	id := flags.String("id", "", "resource ID; defaults to directory name")
	version := flags.String("version", "legacy-v10", "resource version")
	compatibility := flags.String("compatibility", ">=0.11.0 <0.12.0", "CampusOS compatibility range")
	source := flags.String("source", "v10-layout-migration", "resource source")
	entry := flags.String("entry", "style.yaml", "resource entry file")
	force := flags.Bool("force", false, "replace an existing resource.json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if directoryArgument == "" && flags.NArg() == 1 {
		directoryArgument = flags.Arg(0)
	} else if flags.NArg() != 0 {
		return errors.New("usage: campusosctl resource adopt <directory> --type <type> [--id id] [--force]")
	}
	if directoryArgument == "" || *kind == "" {
		return errors.New("usage: campusosctl resource adopt <directory> --type <type> [--id id] [--force]")
	}
	directory := filepath.Clean(directoryArgument)
	resourceType := platformresource.Type(strings.TrimSpace(*kind))
	if platformresource.TypeDirectory(resourceType) == "" {
		return fmt.Errorf("unsupported resource type %q", *kind)
	}
	resourceID := strings.TrimSpace(*id)
	if resourceID == "" {
		resourceID = filepath.Base(directory)
	}
	if !platformresource.SafeID(resourceID) {
		return fmt.Errorf("unsafe resource ID %q", resourceID)
	}
	manifestPath := filepath.Join(directory, "resource.json")
	if _, err := os.Stat(manifestPath); err == nil && !*force {
		return fmt.Errorf("%s already exists; use --force to refresh its checksum", manifestPath)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	checksum, err := platformresource.DirectoryChecksum(directory)
	if err != nil {
		return err
	}
	manifest := platformresource.Manifest{
		Schema: "campusos.resource/v1", ID: resourceID, Type: resourceType,
		Version: strings.TrimSpace(*version), Compatibility: strings.TrimSpace(*compatibility),
		Entry: filepath.ToSlash(filepath.Clean(*entry)), Checksum: checksum, Source: strings.TrimSpace(*source),
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o640); err != nil {
		return err
	}
	if err := platformresource.Validate(directory, manifest); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "adopted resource: %s (%s)\n", resourceID, checksum)
	return nil
}

func runResourceInspect(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: campusosctl resource inspect <directory>")
	}
	directory := filepath.Clean(args[0])
	data, err := os.ReadFile(filepath.Join(directory, "resource.json"))
	if err != nil {
		return err
	}
	var manifest platformresource.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if err := platformresource.Validate(directory, manifest); err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(manifest)
}

func printResourceUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: campusosctl resource <command>")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "commands:")
	fmt.Fprintln(writer, "  adopt    add or refresh a validated resource.json")
	fmt.Fprintln(writer, "  inspect  validate and print a Resource Package manifest")
}
