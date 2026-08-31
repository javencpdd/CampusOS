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

	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
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
	case "adopt-legacy":
		err = runResourceAdoptLegacy(args[1:], stdout)
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
	compatibility := flags.String("compatibility", ">=0.13.0 <0.14.0", "CampusOS compatibility range")
	source := flags.String("source", "v10-layout-migration", "resource source")
	entry := flags.String("entry", "style.yaml", "resource entry file")
	force := flags.Bool("force", false, "replace an existing resource.json")
	legacyReadOnly := flags.Bool("legacy-readonly", false, "preserve a safe pre-v0.13 appearance package as non-applicable legacy data")
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
	validation, err := validateAppearanceResource(directory, resourceType, !*legacyReadOnly)
	if err != nil {
		return err
	}
	if *legacyReadOnly {
		if !isAppearanceResource(resourceType) {
			return errors.New("--legacy-readonly is only valid for appearance resources")
		}
		if validation.DeliveryStatus != stylepack.DeliveryStatusLegacyReadOnly {
			return errors.New("--legacy-readonly only preserves a safe package that lacks the current appearance delivery contract")
		}
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

// runResourceAdoptLegacy exists only for the v0.10 layout migration. It preserves
// a safe old Appearance package as legacy-readonly; it never makes that package
// eligible for import or application.
func runResourceAdoptLegacy(args []string, stdout io.Writer) error {
	return runResourceAdopt(append(args, "--legacy-readonly"), stdout)
}

func validateAppearanceResource(directory string, kind platformresource.Type, strictDelivery bool) (stylepack.ValidationResult, error) {
	expectedTarget := map[platformresource.Type]string{
		platformresource.Theme:          stylepack.TargetWeb,
		platformresource.HomepagePack:   stylepack.TargetHomepage,
		platformresource.SpaceStylePack: stylepack.TargetPersonalSpace,
	}[kind]
	if expectedTarget == "" {
		return stylepack.ValidationResult{}, nil
	}
	var pack *stylepack.Package
	var validation stylepack.ValidationResult
	if strictDelivery {
		pack, validation = stylepack.LoadDirStrict(directory)
	} else {
		pack, validation = stylepack.LoadDir(directory)
	}
	if !validation.Valid || pack == nil {
		return validation, fmt.Errorf("appearance delivery contract: %s", strings.Join(validation.Errors, "; "))
	}
	if pack.Manifest.Target != expectedTarget {
		return validation, fmt.Errorf("appearance package target must be %s", expectedTarget)
	}
	return validation, nil
}

func isAppearanceResource(kind platformresource.Type) bool {
	return kind == platformresource.Theme || kind == platformresource.HomepagePack || kind == platformresource.SpaceStylePack
}

func runResourceInspect(args []string, stdout io.Writer) error {
	directoryArgument := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		directoryArgument = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("resource inspect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	strict := flags.Bool("strict", false, "reject legacy Appearance packages")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if directoryArgument == "" && flags.NArg() == 1 {
		directoryArgument = flags.Arg(0)
	} else if flags.NArg() != 0 || directoryArgument == "" {
		return errors.New("usage: campusosctl resource inspect <directory> [--strict]")
	}
	directory := filepath.Clean(directoryArgument)
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
	validation, err := validateAppearanceResource(directory, manifest.Type, *strict)
	if err != nil {
		return err
	}
	result := struct {
		platformresource.Manifest
		DeliveryStatus   string                      `json:"delivery_status,omitempty"`
		DeliveryIssues   []stylepack.ValidationIssue `json:"delivery_issues,omitempty"`
		DeliveryWarnings []string                    `json:"delivery_warnings,omitempty"`
	}{
		Manifest:         manifest,
		DeliveryStatus:   validation.DeliveryStatus,
		DeliveryIssues:   validation.Issues,
		DeliveryWarnings: validation.Warnings,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func printResourceUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: campusosctl resource <command>")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "commands:")
	fmt.Fprintln(writer, "  adopt    add or refresh a validated resource.json")
	fmt.Fprintln(writer, "  adopt-legacy  preserve a safe v0.10 Appearance package as legacy-readonly during layout migration")
	fmt.Fprintln(writer, "  inspect  validate and print a Resource Package manifest and Appearance delivery status")
}
