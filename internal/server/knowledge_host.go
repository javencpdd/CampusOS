package server

import (
	"context"
	"encoding/json"

	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/plugin"
)

type pluginKnowledgeGate struct{ module *pluginPlatformModule }

func (g pluginKnowledgeGate) Enabled(id string) bool {
	return id == knowledge.FeatureID && g.module != nil && g.module.features != nil && g.module.features.Registry() != nil && g.module.features.Registry().Enabled(id)
}

// Resolve late: the core Runtime starts before optional Feature modules. Only
// the composition root bridges the two; plugins cannot import domain internals.
func (m *pluginPlatformModule) bindKnowledgeHost() error {
	read := func(ctx context.Context, code string, raw json.RawMessage) (json.RawMessage, error) {
		var q struct {
			CollectionID string `json:"collection_id"`
			Query        string `json:"query"`
			Page         int    `json:"page"`
			PageSize     int    `json:"page_size"`
			MemberID     string `json:"member_id"`
			Revision     string `json:"revision"`
		}
		if json.Unmarshal(raw, &q) != nil {
			return nil, plugin.ErrV5CallInvalid
		}
		var out any
		var err error
		switch code {
		case plugin.V5KnowledgeSearch:
			v, ok := m.app.Lookup(knowledge.PortPublicRetrieval)
			if !ok {
				return nil, plugin.ErrV5Denied
			}
			port, ok := v.(knowledge.PublicRetrieval)
			if !ok || port == nil {
				return nil, plugin.ErrV5Denied
			}
			out, err = port.SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: q.CollectionID, Query: q.Query, Page: q.Page, PageSize: q.PageSize})
		case plugin.V5KnowledgeSourceRead:
			v, ok := m.app.Lookup(knowledge.PortCollections)
			if !ok {
				return nil, plugin.ErrV5Denied
			}
			port, ok := v.(*knowledge.CollectionService)
			if !ok || port == nil {
				return nil, plugin.ErrV5Denied
			}
			out, err = port.Source(ctx, q.CollectionID, q.MemberID, q.Revision)
		default:
			return nil, plugin.ErrV5CallTarget
		}
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	}
	for _, code := range []string{plugin.V5KnowledgeSearch, plugin.V5KnowledgeSourceRead} {
		if err := m.managed.BindHost(code, plugin.V5PublicKnowledgeEndpoint(code, read, pluginKnowledgeGate{m})); err != nil {
			return err
		}
	}
	return nil
}
