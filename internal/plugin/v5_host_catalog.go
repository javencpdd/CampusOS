package plugin

import (
	"context"
	"encoding/json"
)

// HostCatalogPage is a host inventory Port; it does not publish contributions
// or authorize dispatch. It returns approved, installed global versions only.
type V5HostCatalogEntry struct{ ID, Version, Runtime, Status string }

func (s *V5Platform) HostCatalogPage(ctx context.Context, after string) ([]V5HostCatalogEntry, string, error) {
	if s == nil || s.store == nil {
		return nil, "", ErrV5PlatformUnavailable
	}
	rows, err := s.store.list(ctx, v5Global, v5Query{After: after, Limit: 50, Visible: true})
	if err != nil {
		return nil, "", err
	}
	out := []V5HostCatalogEntry{}
	next := ""
	for _, raw := range rows {
		var i V5Installation
		if json.Unmarshal(raw, &i) != nil {
			return nil, "", ErrV5PlatformUnavailable
		}
		r, err := s.release(ctx, i.coordinate())
		if err != nil {
			return nil, "", err
		}
		m, err := ParsePluginV5Manifest(r.Manifest)
		if err != nil {
			return nil, "", err
		}
		out = append(out, V5HostCatalogEntry{i.coordinate().family(), i.Version, m.Runtime, i.Observed})
		next = v5RecordID(v5Global, raw)
	}
	if len(rows) < 50 {
		next = ""
	}
	return out, next, nil
}
