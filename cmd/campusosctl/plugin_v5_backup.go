package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"

	"github.com/campusos/CampusOS/internal/plugin"
)

func backupPluginV5(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("plugin backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	inventory := fs.String("inventory", "", "operator-exported JSON object containing releases, without user data")
	directory := fs.String("data-dir", "", "existing host-managed v5 data directory")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *inventory == "" || *directory == "" {
		return plugin.ErrV5Invalid
	}
	raw, err := readV5SmallFile(*inventory)
	if err != nil {
		return err
	}
	var input struct {
		Releases []plugin.V5PackageRelease `json:"releases"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || len(input.Releases) < 1 || len(input.Releases) > 256 {
		return plugin.ErrV5Invalid
	}
	files, err := plugin.NewV5ManagedFiles(*directory)
	if err != nil {
		return err
	}
	defer files.Close()
	type summary struct {
		plugin.V5Coordinate
		Digest string `json:"package_digest"`
		Size   int    `json:"size_bytes"`
	}
	packages := []summary{}
	for _, release := range input.Releases {
		if err := files.BackupPackage(release); err != nil {
			return err
		}
		packages = append(packages, summary{release.V5Coordinate, release.Digest, release.Size})
	}
	return json.NewEncoder(out).Encode(struct {
		Contract string    `json:"contract"`
		Verified bool      `json:"backup_verified"`
		Packages []summary `json:"packages"`
	}{"campusos.plugin-repository/v1", true, packages})
}
