package plugin

import (
	"bytes"
	"context"
)

// V5WasmProgram is an immutable artifact captured under current catalog trust.
// It is not an installation, Grant, current authorization or publication proof.
// Only the catalog can construct a nonzero program; no filesystem path escapes.
type V5WasmProgram struct {
	code       []byte
	digest     string
	coordinate V5Coordinate
	resources  PluginV5ManifestResources
	consumes   []PluginV5Consume
}

func (p V5WasmProgram) Bytes() []byte            { return bytes.Clone(p.code) }
func (p V5WasmProgram) Digest() string           { return p.digest }
func (p V5WasmProgram) Coordinate() V5Coordinate { return p.coordinate }
func (p V5WasmProgram) Resources() PluginV5ManifestResources {
	r := p.resources
	r.NetworkTargets = append([]string(nil), r.NetworkTargets...)
	return r
}

func (p V5WasmProgram) Consumes() []PluginV5Consume {
	out := append([]PluginV5Consume(nil), p.consumes...)
	for n := range out {
		out[n].Scope.CollectionIDs = append([]string(nil), out[n].Scope.CollectionIDs...)
		out[n].Scope.Keys = append([]string(nil), out[n].Scope.Keys...)
	}
	return out
}

// ParseV5WasmHostRequest accepts only a finite capability identifier and its
// JSON payload, never an authority/envelope supplied by guest memory.
func ParseV5WasmHostRequest(raw []byte) (string, []byte, error) {
	v, ok := v5JSON(raw)
	if !ok || !v5Object("capability payload", map[string]v5Check{"capability": v5Capability, "payload": func(v any) bool { _, ok := v.(map[string]any); return ok }})(v) {
		return "", nil, ErrV5CallInvalid
	}
	m := v.(map[string]any)
	wire, err := v5Marshal(v5CanonicalNumbers(m["payload"]))
	return m["capability"].(string), wire, err
}

// CaptureWasmProgram re-verifies the signed archive and write-once release
// identity, then copies only the declared host-call Wasm artifact. Compilation
// and guest execution must happen after the catalog lock has been released.
// The managed lifecycle coordinator must recheck current facts before dispatch.
func (c *PluginV5PackageCatalog) CaptureWasmProgram(ctx context.Context, archive, release []byte) (V5WasmProgram, error) {
	var out V5WasmProgram
	err := c.WithVerified(ctx, archive, release, func(p *PluginV5Package) error {
		m := p.Manifest
		if m.Runtime != "wasm" || m.Backend == nil || m.Backend.Transport != "host-call" || m.Backend.Artifact.Kind != "wasm" || m.HostResources == nil {
			return ErrPluginV5ManifestShape
		}
		a := m.Backend.Artifact
		raw := p.files[a.Path]
		if len(raw) == 0 || v5BytesDigest(raw) != a.Digest {
			return ErrPluginV5PackageDigest
		}
		r := *m.HostResources
		r.NetworkTargets = append([]string(nil), r.NetworkTargets...)
		out = V5WasmProgram{code: bytes.Clone(raw), digest: a.Digest, coordinate: V5Coordinate{m.Publisher, m.Key, m.Version}, resources: r, consumes: m.Consumes}
		return nil
	})
	if err != nil {
		return V5WasmProgram{}, err
	}
	return out, nil
}
