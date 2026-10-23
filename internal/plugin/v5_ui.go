package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// UI availability is independent of backend health. In particular a verified
// UI-only package does not create a synthetic ready Runner observation.
type V5UIFacts struct {
	V5Coordinate
	PackageDigest    string             `json:"package_digest"`
	Generation       string             `json:"generation"`
	GlobalGeneration string             `json:"global_generation"`
	Proof            string             `json:"proof"`
	AuthorityProof   string             `json:"authority_proof"`
	Artifact         PluginV5UIArtifact `json:"artifact"`
}

func (s *V5Platform) withUI(ctx context.Context, c V5Coordinate, admin bool, resolve identity.SessionPrincipalResolver, body func(V5UIFacts, *PluginV5Package) error) error {
	if !c.valid() {
		return ErrV5Invalid
	}
	release, err := s.release(ctx, c)
	if err != nil {
		return err
	}
	if release.Storage != "available" {
		return ErrV5PlatformUnavailable
	}
	raw, err := s.files.Package(release.Digest)
	if err != nil {
		return err
	}
	return s.catalog.WithVerified(ctx, raw, release.Release, func(p *PluginV5Package) error {
		if p.Digest != release.Digest {
			return ErrPluginV5PackageDigest
		}
		return s.read(ctx, admin, resolve, func(tx context.Context, a v5Actor) error {
			if err := s.store.lock(tx, c.family()); err != nil {
				return err
			}
			current, err := s.release(tx, c)
			if err != nil {
				return err
			}
			if current.Digest != release.Digest || current.Storage != "available" {
				return ErrV5Conflict
			}
			global, err := s.installation(tx, c, "")
			if err != nil {
				return err
			}
			usable := func(i V5Installation) bool {
				return i.Installed && i.Version == c.Version && i.DesiredVersion == c.Version && i.Desired == "enabled" && (p.Manifest.Runtime == "none" || i.Observed == "ready" && i.ActiveGeneration == i.Generation)
			}
			if !usable(global) {
				return ErrV5Denied
			}
			if global.DataGeneration > 0 {
				view, err := s.files.dataView(global, global.DataGeneration)
				if err != nil {
					return err
				}
				view.Close()
			}
			grant, err := s.grant(tx, c)
			if err != nil {
				return err
			}
			if !v5GrantCompleteAt(p.Manifest, grant, s.now().UnixMilli()) {
				return ErrV5Denied
			}
			owner := ""
			installation := global
			if !admin {
				owner = a.p.Actor.ID
				installation, err = s.installation(tx, c, owner)
				if err != nil {
					return err
				}
				if !usable(installation) || !s.consentOK(tx, c, owner, p.Manifest, grant) {
					return ErrV5Denied
				}
				if installation.DataGeneration > 0 {
					view, err := s.files.dataView(installation, installation.DataGeneration)
					if err != nil {
						return err
					}
					view.Close()
				}
			}
			if err := s.validateInstanceConfiguration(tx, c, owner, p.Manifest); err != nil {
				return err
			}
			audience := "user"
			if admin {
				audience = "admin"
			}
			var artifact *PluginV5UIArtifact
			for _, ui := range p.Manifest.UI {
				if ui.Audience == audience {
					copy := ui
					artifact = &copy
					break
				}
			}
			if artifact == nil {
				return ErrV5NotFound
			}
			// The proof binds the current session, both install generations, Grant,
			// Consent and configuration revisions. Any mutation fences old contexts.
			entry, err := s.entry(tx, c, a, true)
			if err != nil {
				return err
			}
			cfg, err := s.configuration(tx, c, "", p.Manifest)
			if err != nil {
				return err
			}
			binding := struct {
				Principal                                                         identity.PrincipalContext
				PackageDigest, GlobalGeneration, Generation, SystemConfigRevision string
				Grant                                                             V5Grant
				Consent                                                           *V5Consent
				ConfigRevision                                                    string
			}{a.p, p.Digest, v5Counter(global.Generation), v5Counter(installation.Generation), v5Counter(cfg.Revision), grant, entry.Consent, func() string {
				if entry.Configuration != nil {
					return v5Counter(entry.Configuration.Revision)
				}
				return "0"
			}()}
			proof, _ := json.Marshal(binding)
			// Resource contexts belong to their current owner, and can be opened
			// after a valid session rotation (including the same owner's new tab).
			// Asset leases and Bridge sessions additionally pin the credential.
			binding.Principal.CredentialID = ""
			binding.Principal.AuthenticationStrength = ""
			authority, _ := json.Marshal(binding)
			facts := V5UIFacts{V5Coordinate: c, PackageDigest: p.Digest, Generation: v5Counter(installation.Generation), GlobalGeneration: v5Counter(global.Generation), Proof: v5BytesDigest(proof), AuthorityProof: v5BytesDigest(authority), Artifact: *artifact}
			return body(facts, p)
		})
	})
}

func (s *V5Platform) UI(ctx context.Context, c V5Coordinate, admin bool, resolve identity.SessionPrincipalResolver) (V5UIFacts, error) {
	var out V5UIFacts
	if s == nil || s.files == nil || s.catalog == nil {
		return out, ErrV5PlatformUnavailable
	}
	err := s.withUI(ctx, c, admin, resolve, func(f V5UIFacts, _ *PluginV5Package) error { out = f; return nil })
	return out, err
}

// UIAsset returns only immutable audience-local assets. It never reads an
// arbitrary filesystem path or serves a package's manifest/configuration.
func (s *V5Platform) UIAsset(ctx context.Context, expected V5UIFacts, name string, admin bool, resolve identity.SessionPrincipalResolver) ([]byte, string, error) {
	var out []byte
	mime := ""
	if !v5PackagePath(name) || (!strings.HasPrefix(name, path.Dir(expected.Artifact.Entrypoint)+"/") && !strings.HasPrefix(name, "dist/assets/")) {
		return nil, "", ErrV5Denied
	}
	switch path.Ext(name) {
	case ".html":
		mime = "text/html; charset=utf-8"
	case ".js", ".mjs":
		mime = "text/javascript; charset=utf-8"
	case ".css":
		mime = "text/css; charset=utf-8"
	case ".wasm":
		mime = "application/wasm"
	case ".png":
		mime = "image/png"
	case ".svg":
		mime = "image/svg+xml"
	case ".woff2":
		mime = "font/woff2"
	default:
		return nil, "", ErrV5Denied
	}
	err := s.withUI(ctx, expected.V5Coordinate, admin, resolve, func(f V5UIFacts, p *PluginV5Package) error {
		if f != expected {
			return ErrV5Conflict
		}
		raw, ok := p.files[name]
		if !ok {
			return ErrV5NotFound
		}
		out = append([]byte(nil), raw...)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return out, mime, nil
}

// PDF is a compiled adapter for the published, resource-selected UI contract.
// Resource ownership/classification remains with the three resource services.
func (s *V5Platform) PDFUI(ctx context.Context, user string, resolve identity.SessionPrincipalResolver) (V5UIFacts, error) {
	if s == nil || resolve == nil {
		return V5UIFacts{}, ErrV5Denied
	}
	p, err := resolve(ctx)
	if err != nil || p.Actor.Kind != "user" || p.Actor.ID != user {
		return V5UIFacts{}, ErrV5Denied
	}
	c := V5Coordinate{Publisher: "campusos", Key: "pdf-viewer"}
	i, err := s.installation(ctx, c, "")
	if err != nil {
		return V5UIFacts{}, err
	}
	c.Version = i.Version
	facts, err := s.UI(ctx, c, false, resolve)
	if err != nil {
		return facts, err
	}
	detail, err := s.Detail(ctx, c, false, resolve)
	if err != nil {
		return facts, err
	}
	m, err := ParsePluginV5Manifest(detail.Release.Manifest)
	if err != nil {
		return facts, err
	}
	for _, code := range []string{"ui.resource.selected.read", "ui.surface.open"} {
		found := false
		for _, consume := range detail.Grant.Consumes {
			if consume.Capability == code && consume.Scope.Kind == "self" {
				found = true
			}
		}
		declared := false
		for _, consume := range m.Consumes {
			if consume.Capability == code && consume.Scope.Kind == "self" && consume.Required {
				declared = true
			}
		}
		if !found || !declared {
			return facts, ErrV5Denied
		}
	}
	// Re-read after the second projection; callers may pin this proof in their
	// resource invocation, and subsequent reads must reproduce it exactly.
	after, err := s.UI(ctx, c, false, resolve)
	if err != nil || after != facts {
		if err == nil {
			err = ErrV5Conflict
		}
		return V5UIFacts{}, err
	}
	return facts, nil
}

func v5UIUnavailable(err error) bool {
	return errors.Is(err, ErrV5Denied) || errors.Is(err, ErrV5NotFound) || errors.Is(err, ErrV5PlatformUnavailable) || errors.Is(err, ErrV5Conflict) || errors.Is(err, ErrV5Rollback)
}
