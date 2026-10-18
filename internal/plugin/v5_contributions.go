package plugin

import (
	"context"
	"encoding/json"
	"errors"
)

// These are host-owned extension points, separate from legacy capability maps.
type V5ContributionPoint string

const (
	V5PreviewRenderer V5ContributionPoint = "preview.renderer"
	V5AIChat          V5ContributionPoint = "ai.chat"
	V5KnowledgeSource V5ContributionPoint = "knowledge.source"
)

var (
	ErrV5ContributionUnpublished = errors.New("plugin.target_unpublished")
	ErrV5ContributionConflict    = errors.New("plugin.contribution_selection_conflict")
	ErrV5ContributionUnsupported = errors.New("plugin.extension_point_unsupported")
	ErrV5ContributionStale       = errors.New("plugin.instance_stale")
	ErrV5ContributionBudget      = errors.New("plugin.call_budget")
)

type V5ContributionQuery struct {
	Point           V5ContributionPoint
	ContractVersion string
	Capability      string
}

// A reference includes the release coordinate; neither a local provides ID nor
// installation order is a global binding. A selected release never falls back.
type V5ContributionRef struct {
	V5Coordinate
	ID string `json:"id"`
}

type V5ContributionDescriptor struct {
	V5ContributionRef
	Point              V5ContributionPoint `json:"point"`
	ContractVersion    string              `json:"contract_version"`
	Capabilities       []string            `json:"capabilities"`
	DataMode           string              `json:"data_mode"`
	InstanceGeneration int64               `json:"instance_generation,string"`
	GlobalGeneration   int64               `json:"global_generation,string"`
}

// Binding is an opaque, process-local lookup snapshot, not an execution grant
// or wire credential. Future dispatch must recheck it AND enforce the feature,
// resource ACL, instance credential and per-call budgets before invocation.
type V5ContributionBinding struct {
	descriptor    V5ContributionDescriptor
	owner         string
	query         V5ContributionQuery
	explicit      bool
	grantRevision string
}

func (b V5ContributionBinding) Descriptor() V5ContributionDescriptor {
	d := b.descriptor
	d.Capabilities = append([]string(nil), d.Capabilities...)
	return d
}

// Directory derives metadata from durable03b facts and verified packages on
// every lookup. It has no registration callback, provider cache or default
// binding to resurrect after restart. owner is supplied by a compiled host
// adapter from its current identity context, never an untrusted plugin field.
type V5ContributionDirectory struct{ platform *V5Platform }

func NewV5ContributionDirectory(platform *V5Platform) *V5ContributionDirectory {
	return &V5ContributionDirectory{platform: platform}
}

func v5ContributionQuerySupported(q V5ContributionQuery) bool {
	_, wire := CurrentPluginV5Catalogs()
	var catalog v5ExtensionManifestCatalog
	if json.Unmarshal(wire, &catalog) != nil {
		return false
	}
	for _, p := range catalog.Points {
		if p.Point == string(q.Point) && p.ContractVersion == q.ContractVersion && v5Contains(p.Capabilities, q.Capability) {
			return true
		}
	}
	return false
}

// Resolve returns one authorized, ready declaration. Multiple matches require
// an exact host-selected reference; there is no implicit first/last winner.
// Discovery is bounded to20 pages of50 installed, approved families; exhausting
// that budget fails closed, including when only one match has been seen.
func (d *V5ContributionDirectory) Resolve(ctx context.Context, owner string, q V5ContributionQuery, selected *V5ContributionRef) (V5ContributionBinding, error) {
	zero := V5ContributionBinding{}
	if d == nil || d.platform == nil || d.platform.store == nil || d.platform.files == nil || d.platform.catalog == nil || d.platform.reliable == nil {
		return zero, ErrV5PlatformUnavailable
	}
	if !v5OwnerValid(owner) || selected != nil && (!selected.V5Coordinate.valid() || !v5Name(selected.ID)) {
		return zero, ErrV5Invalid
	}
	if !v5ContributionQuerySupported(q) {
		return zero, ErrV5ContributionUnsupported
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if selected != nil {
		items, err := d.candidates(ctx, selected.V5Coordinate, owner, q)
		if err != nil {
			return zero, err
		}
		for _, b := range items {
			if b.descriptor.V5ContributionRef == *selected {
				b.explicit = true
				return b, nil
			}
		}
		return zero, ErrV5ContributionUnpublished
	}
	var found V5ContributionBinding
	count, after := 0, ""
	for page := 0; page < 20; page++ {
		rows, err := d.platform.store.list(ctx, v5Global, v5Query{After: after, Limit: 50, Visible: true})
		if err != nil {
			return zero, ErrV5PlatformUnavailable
		}
		for _, raw := range rows {
			var i V5Installation
			if json.Unmarshal(raw, &i) != nil || !i.coordinate().valid() {
				return zero, ErrV5PlatformUnavailable
			}
			items, err := d.candidates(ctx, i.coordinate(), owner, q)
			if err != nil {
				return zero, err
			}
			for _, b := range items {
				count++
				if count > 1 {
					return zero, ErrV5ContributionConflict
				}
				found = b
			}
			next := v5RecordID(v5Global, raw)
			if next <= after {
				return zero, ErrV5PlatformUnavailable
			}
			after = next
		}
		if len(rows) < 50 {
			if count == 0 {
				return zero, ErrV5ContributionUnpublished
			}
			return found, nil
		}
	}
	return zero, ErrV5ContributionBudget
}

func (d *V5ContributionDirectory) candidates(ctx context.Context, c V5Coordinate, owner string, q V5ContributionQuery) ([]V5ContributionBinding, error) {
	s := d.platform
	facts, err := s.ExecutionFacts(ctx, c, owner)
	if errors.Is(err, ErrV5NotFound) || errors.Is(err, ErrV5Denied) {
		return nil, nil
	}
	if err != nil {
		// Fail closed without exporting filesystem, trust or storage details.
		return nil, ErrV5PlatformUnavailable
	}
	if !facts.ReadyForDispatch {
		return nil, nil
	}
	r, err := s.release(ctx, c)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	// Release manifests are immutable and were checked against these signed
	// bytes at import. ExecutionFacts re-verifies current bytes/trust above.
	m, err := ParsePluginV5Manifest(r.Manifest)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	out := []V5ContributionBinding{}
	for _, p := range m.Provides {
		if p.Point != string(q.Point) || p.ContractVersion != q.ContractVersion || !v5Contains(p.Capabilities, q.Capability) {
			continue
		}
		out = append(out, V5ContributionBinding{
			descriptor: V5ContributionDescriptor{
				V5ContributionRef: V5ContributionRef{c, p.ID}, Point: q.Point, ContractVersion: p.ContractVersion,
				Capabilities: append([]string(nil), p.Capabilities...), DataMode: p.DataMode,
				InstanceGeneration: facts.Installation.ActiveGeneration, GlobalGeneration: facts.Global.ActiveGeneration,
			}, owner: owner, query: q, grantRevision: facts.GrantRevision,
		})
	}
	return out, nil
}

// Recheck never refreshes a stale binding in place. Automatic selection also
// rechecks uniqueness, so a newly ready competitor cannot silently replace it.
func (d *V5ContributionDirectory) Recheck(ctx context.Context, b V5ContributionBinding) error {
	if b.descriptor.InstanceGeneration < 1 || b.descriptor.GlobalGeneration < 1 {
		return ErrV5ContributionStale
	}
	var selected *V5ContributionRef
	if b.explicit {
		ref := b.descriptor.V5ContributionRef
		selected = &ref
	}
	current, err := d.Resolve(ctx, b.owner, b.query, selected)
	if err != nil {
		return err
	}
	a, z := b.descriptor, current.descriptor
	if a.V5ContributionRef != z.V5ContributionRef || a.InstanceGeneration != z.InstanceGeneration || a.GlobalGeneration != z.GlobalGeneration || b.grantRevision != current.grantRevision {
		return ErrV5ContributionStale
	}
	return nil
}
