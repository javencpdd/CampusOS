package plugin

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// V5ContainerProgram captures a signed declaration, not an execution grant.
// It contains no daemon, filesystem path, endpoint or installation credential.
// Image resolution and later runtime IO must run outside the catalog lock.
type V5ContainerProgram struct {
	image, transport, packageDigest string
	coordinate                      V5Coordinate
	resources                       PluginV5ManifestResources
	consumes                        []PluginV5Consume
}

func (p V5ContainerProgram) ImageReference() string   { return p.image }
func (p V5ContainerProgram) Transport() string        { return p.transport }
func (p V5ContainerProgram) PackageDigest() string    { return p.packageDigest }
func (p V5ContainerProgram) Coordinate() V5Coordinate { return p.coordinate }
func (p V5ContainerProgram) Resources() PluginV5ManifestResources {
	r := p.resources
	r.NetworkTargets = append([]string(nil), r.NetworkTargets...)
	return r
}
func (p V5ContainerProgram) Consumes() []PluginV5Consume {
	out := append([]PluginV5Consume(nil), p.consumes...)
	for n := range out {
		out[n].Scope.CollectionIDs = append([]string(nil), out[n].Scope.CollectionIDs...)
		out[n].Scope.Keys = append([]string(nil), out[n].Scope.Keys...)
	}
	return out
}

var v5ContainerDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var v5ContainerRegistryLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
var v5ContainerRepositoryPart = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)
var v5ContainerPort = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)

// Deployment subset of the frozen wire syntax: explicit registry, canonical
// lowercase repository, no tag/default registry/scheme/options or normalization.
// This parser performs no DNS, registry, daemon or filesystem access.
func v5ContainerImageReference(ref string) bool {
	if len(ref) < 1 || len(ref) > 512 || strings.Count(ref, "@") != 1 {
		return false
	}
	name, digest, _ := strings.Cut(ref, "@")
	if !v5ContainerDigest.MatchString(digest) {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return false
	}
	registry := parts[0]
	if strings.Count(registry, ":") > 1 {
		return false
	}
	if host, port, found := strings.Cut(registry, ":"); found {
		n, err := strconv.Atoi(port)
		if !v5ContainerPort.MatchString(port) || err != nil || n > 65535 {
			return false
		}
		registry = host
	}
	if len(registry) > 253 || registry != "localhost" && !strings.Contains(registry, ".") {
		return false
	}
	for _, label := range strings.Split(registry, ".") {
		if len(label) > 63 || !v5ContainerRegistryLabel.MatchString(label) {
			return false
		}
	}
	for _, part := range parts[1:] {
		if !v5ContainerRepositoryPart.MatchString(part) {
			return false
		}
	}
	return true
}

// CaptureContainerProgram rechecks current signature trust and the write-once
// publisher/key/version identity. It never pulls, installs, starts or publishes.
func (c *PluginV5PackageCatalog) CaptureContainerProgram(ctx context.Context, archive, release []byte) (V5ContainerProgram, error) {
	var out V5ContainerProgram
	err := c.WithVerified(ctx, archive, release, func(p *PluginV5Package) error {
		m := p.Manifest
		if m.Runtime != "container" || m.Backend == nil || m.Backend.Artifact.Kind != "container" || m.HostResources == nil || m.Backend.Transport != "http" && m.Backend.Transport != "grpc" || !v5ContainerImageReference(m.Backend.Artifact.Image) {
			return ErrPluginV5ManifestShape
		}
		r := *m.HostResources
		r.NetworkTargets = append([]string(nil), r.NetworkTargets...)
		out = V5ContainerProgram{image: m.Backend.Artifact.Image, transport: m.Backend.Transport, packageDigest: p.Digest, coordinate: V5Coordinate{m.Publisher, m.Key, m.Version}, resources: r, consumes: m.Consumes}
		return nil
	})
	if err != nil {
		return V5ContainerProgram{}, err
	}
	return out, nil
}
