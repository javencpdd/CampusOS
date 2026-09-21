package v4

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// InstalledRelease is a validated, immutable on-disk release. It contains no
// execution hook: the host decides lifecycle and later chooses an appropriate
// runtime adapter after this verification step has completed.
type InstalledRelease struct {
	Manifest     *Manifest
	Path         string
	Digest       string
	SignatureKey string
}

// DiscoverInstalledReleases reads only plugins/.installed. Repository source
// folders and .staging are intentionally ignored, so editing a checked-out
// plugin cannot make it runnable before the explicit package/install flow.
func DiscoverInstalledReleases(root string) ([]InstalledRelease, error) {
	root, err := installationRoot(root)
	if err != nil {
		return nil, err
	}
	installedRoot := filepath.Join(root, ".installed")
	keys, err := os.ReadDir(installedRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []InstalledRelease{}, nil
	}
	if err != nil {
		return nil, err
	}
	releases := make([]InstalledRelease, 0)
	for _, key := range keys {
		if !key.IsDir() || !pluginKeyPattern.MatchString(key.Name()) {
			continue
		}
		versions, err := os.ReadDir(filepath.Join(installedRoot, key.Name()))
		if err != nil {
			return nil, err
		}
		for _, version := range versions {
			if !version.IsDir() || strings.HasPrefix(version.Name(), ".") {
				continue
			}
			pathname := filepath.Join(installedRoot, key.Name(), version.Name())
			release, err := loadInstalledRelease(pathname)
			if err != nil {
				return nil, fmt.Errorf("inspect v4 installed release %s: %w", pathname, err)
			}
			if release.Manifest.Key != key.Name() {
				return nil, fmt.Errorf("installed release key directory does not match manifest: %s", pathname)
			}
			releases = append(releases, release)
		}
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].Manifest.Key != releases[j].Manifest.Key {
			return releases[i].Manifest.Key < releases[j].Manifest.Key
		}
		if releases[i].Manifest.Version != releases[j].Manifest.Version {
			return releases[i].Manifest.Version < releases[j].Manifest.Version
		}
		return releases[i].Digest < releases[j].Digest
	})
	return releases, nil
}

func loadInstalledRelease(pathname string) (InstalledRelease, error) {
	manifest, err := ValidateReleaseDirectory(pathname)
	if err != nil {
		return InstalledRelease{}, err
	}
	checksumsData, err := os.ReadFile(filepath.Join(pathname, ReleaseChecksumsFile))
	if err != nil {
		return InstalledRelease{}, errors.New("installed v4 release is missing checksums.json")
	}
	var checksums ReleaseChecksums
	if err := json.Unmarshal(checksumsData, &checksums); err != nil || checksums.Version != "campusos.release-checksums/v1" {
		return InstalledRelease{}, errors.New("installed v4 release checksums are invalid")
	}
	_, calculated, err := collectReleaseFiles(pathname)
	if err != nil || !sameChecksums(checksums.Files, calculated) {
		return InstalledRelease{}, errors.New("installed v4 release checksum verification failed")
	}
	sum := sha256.Sum256(checksumsData)
	release := InstalledRelease{Manifest: manifest, Path: filepath.Clean(pathname), Digest: "sha256:" + hex.EncodeToString(sum[:])}
	if signatureData, readErr := os.ReadFile(filepath.Join(pathname, ReleaseSignatureFile)); readErr == nil {
		var signature ReleaseSignature
		if json.Unmarshal(signatureData, &signature) == nil && signature.Algorithm == "ed25519" {
			release.SignatureKey = signature.KeyID
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return InstalledRelease{}, readErr
	}
	return release, nil
}
