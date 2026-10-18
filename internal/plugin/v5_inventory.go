package plugin

import (
	"context"
	"encoding/json"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// HTTP projections exclude worker leases, request hashes, private preparation
// capsules and other owners' configuration/consent. Catalog visibility is
// applied in the store before pagination, never after a hidden raw page.
type V5OperationView struct {
	V5Coordinate
	ID         string `json:"id"`
	Owner      string `json:"owner_user_id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Generation string `json:"generation"`
	Error      string `json:"error_code"`
	Purge      bool   `json:"purge_data"`
	Attempts   int    `json:"attempts"`
}

func V5PublicOperation(op V5Operation) V5OperationView {
	return V5OperationView{op.V5Coordinate, op.ID, op.Owner, op.Kind, op.Status, v5Counter(op.Generation), op.Error, op.Purge, op.Attempts}
}

type V5InventoryEntry struct {
	Release       V5PackageRelease `json:"release"`
	Installation  *V5Installation  `json:"installation,omitempty"`
	Grant         *V5Grant         `json:"grant,omitempty"`
	Consent       *V5Consent       `json:"consent,omitempty"`
	Configuration *V5Configuration `json:"configuration,omitempty"`
	PurposeHash   string           `json:"purpose_hash"`
}
type V5InventoryPage struct {
	Items []V5InventoryEntry `json:"items"`
	Next  string             `json:"next_cursor"`
}

func (s *V5Platform) read(ctx context.Context, admin bool, resolve identity.SessionPrincipalResolver, body func(context.Context, v5Actor) error) error {
	a, err := s.actor(ctx, admin, "read", resolve, false)
	if err != nil {
		return err
	}
	return s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		if err := s.recheck(tx, a, "read", resolve); err != nil {
			return err
		}
		if err := body(tx, a); err != nil {
			return err
		}
		return s.recheck(tx, a, "read", resolve)
	})
}
func (s *V5Platform) Inventory(ctx context.Context, admin bool, after string, limit int, resolve identity.SessionPrincipalResolver) (V5InventoryPage, error) {
	out := V5InventoryPage{Items: []V5InventoryEntry{}}
	if len(after) > 512 || limit < 1 || limit > 50 {
		return out, ErrV5Invalid
	}
	err := s.read(ctx, admin, resolve, func(tx context.Context, a v5Actor) error {
		kind := v5Release
		if !admin {
			kind = v5Global
		}
		rows, err := s.store.list(tx, kind, v5Query{After: after, Limit: limit + 1, Visible: !admin})
		if err != nil {
			return err
		}
		for n, raw := range rows {
			if n == limit {
				break
			}
			var c V5Coordinate
			if json.Unmarshal(raw, &c) != nil {
				return ErrV5PlatformUnavailable
			}
			if !admin {
				var i V5Installation
				json.Unmarshal(raw, &i)
				c = i.coordinate()
			}
			entry, err := s.entry(tx, c, a, false)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, entry)
			if len(rows) > limit {
				out.Next = v5RecordID(kind, raw)
			}
		}
		return nil
	})
	return out, err
}
func (s *V5Platform) entry(ctx context.Context, c V5Coordinate, a v5Actor, private bool) (V5InventoryEntry, error) {
	out := V5InventoryEntry{}
	r, err := s.release(ctx, c)
	if err != nil {
		return out, err
	}
	owner := ""
	if !a.admin {
		owner = a.p.Actor.ID
	}
	i, ierr := s.installation(ctx, c, owner)
	if ierr != nil && ierr != ErrV5NotFound {
		return out, ierr
	}
	g, gerr := s.grant(ctx, c)
	if gerr != nil && gerr != ErrV5NotFound {
		return out, gerr
	}
	if !a.admin {
		global, e := s.installation(ctx, c, "")
		visible := e == nil && global.Installed && global.Version == c.Version && gerr == nil && g.Approved
		if !visible && !(private && ierr == nil && (i.Version == c.Version || i.DesiredVersion == c.Version)) {
			return out, ErrV5NotFound
		}
	}
	out.Release = r
	if ierr == nil {
		out.Installation = &i
	}
	if gerr == nil {
		out.Grant = &g
	}
	m, err := ParsePluginV5Manifest(r.Manifest)
	if err != nil {
		return out, err
	}
	out.PurposeHash = V5PurposeHash(m)
	if private {
		if owner != "" {
			var consent V5Consent
			e := s.store.load(ctx, v5Consent, v5VersionOwnerID(c, owner), &consent)
			if e == nil {
				out.Consent = &consent
			} else if e != ErrV5NotFound {
				return out, e
			}
		}
		if a.admin && m.Data.ConfigScope != "user" || !a.admin && m.Data.ConfigScope != "system" {
			cfg, e := s.configuration(ctx, c, owner, m)
			if e != nil {
				return out, e
			}
			out.Configuration = &cfg
		}
	}
	return out, nil
}
func (s *V5Platform) Detail(ctx context.Context, c V5Coordinate, admin bool, resolve identity.SessionPrincipalResolver) (V5InventoryEntry, error) {
	out := V5InventoryEntry{}
	if !c.valid() {
		return out, ErrV5Invalid
	}
	err := s.read(ctx, admin, resolve, func(tx context.Context, a v5Actor) error {
		if err := s.store.lock(tx, c.family()); err != nil {
			return err
		}
		var err error
		out, err = s.entry(tx, c, a, true)
		return err
	})
	return out, err
}
func (s *V5Platform) Operation(ctx context.Context, id string, admin bool, resolve identity.SessionPrincipalResolver) (V5OperationView, error) {
	out := V5OperationView{}
	if !v5String(32, 32, `^[0-9a-f]+$`)(id) {
		return out, ErrV5Invalid
	}
	err := s.read(ctx, admin, resolve, func(tx context.Context, a v5Actor) error {
		var op V5Operation
		if err := s.store.load(tx, v5Operation, id, &op); err != nil {
			return err
		}
		if !admin && (op.Owner != a.p.Actor.ID || op.ActorKind != "user" || op.ActorID != a.p.Actor.ID) {
			return ErrV5NotFound
		}
		out = V5PublicOperation(op)
		return nil
	})
	return out, err
}
