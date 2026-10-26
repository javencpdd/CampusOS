package server

import (
	"context"
	"encoding/json"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/plugin"
	sdk "github.com/campusos/CampusOS/sdk/go"
	"time"
)

type knowledgePluginSourceAdapter struct{ owner *Server }

func (a knowledgePluginSourceAdapter) ListPublicPlugin(ctx context.Context, resolve identity.SessionPrincipalResolver, q knowledge.PluginSourceRequest, guard func(context.Context) (string, error)) (knowledge.PluginSourceResult, error) {
	zero := knowledge.PluginSourceResult{}
	if a.owner == nil || a.owner.appContext == nil || guard == nil {
		return zero, knowledge.ErrSourceUnavailable
	}
	p, e := resolve(ctx)
	if e != nil {
		return zero, e
	}
	value, _ := a.owner.appContext.Lookup("plugin.v5.dispatcher")
	hub, ok := value.(*plugin.V5ManagedRuntime)
	if !ok || hub == nil {
		return zero, knowledge.ErrSourceUnavailable
	}
	selected := &plugin.V5ContributionRef{V5Coordinate: plugin.V5Coordinate{Publisher: q.Publisher, Key: q.Key, Version: q.Version}, ID: q.ContributionID}
	b, e := hub.Select(ctx, p.Actor.ID, plugin.V5ContributionQuery{Point: plugin.V5KnowledgeSource, ContractVersion: "v1", Capability: "public_text"}, selected)
	if e != nil {
		return zero, e
	}
	d := b.Descriptor()
	if d.DataMode != "public_only" {
		return zero, knowledge.ErrSourceUnavailable
	}
	request := sdk.V5KnowledgeRequest{RequestID: q.RequestID, ContributionID: d.ID, InstanceGeneration: d.InstanceGeneration, DeadlineMS: q.TimeoutMS, BindingID: q.CollectionID, Cursor: knowledge.PluginSourceCursor(q.Query, q.Page), Limit: int64(q.Limit)}
	payload, e := json.Marshal(request)
	if e != nil {
		return zero, e
	}
	envelope, e := json.Marshal(sdk.V5DispatchCall{Contract: "campusos.plugin-dispatch/v1", RequestID: q.RequestID, Target: sdk.V5DispatchTarget{Kind: "contribution", ID: d.ID}, InstanceGeneration: d.InstanceGeneration, DeadlineMS: time.Now().Add(time.Duration(q.TimeoutMS) * time.Millisecond).UnixMilli(), RequestBytes: int64(len(payload)), ResponseLimitBytes: plugin.V5ChatMaxResponseBytes})
	if e != nil {
		return zero, e
	}
	consumer := plugin.V5Consumer{Feature: a.owner.features, FeatureID: knowledge.FeatureID, Authorize: func(c context.Context, _ identity.PrincipalContext, _ plugin.V5ContributionDescriptor, _ json.RawMessage) (string, error) {
		return guard(c)
	}}
	raw, e := hub.Invoke(ctx, consumer, b, envelope, payload, resolve)
	if e != nil {
		return zero, e
	}
	var out sdk.V5KnowledgeResponse
	if json.Unmarshal(raw, &out) != nil {
		return zero, knowledge.ErrSourceUnavailable
	}
	result := knowledge.PluginSourceResult{Items: []knowledge.PluginSourceItem{}, NextCursor: out.NextCursor}
	for _, v := range out.Items {
		result.Items = append(result.Items, knowledge.PluginSourceItem{ExternalID: v.ExternalID, Revision: v.Revision, Title: v.Title, Text: v.Text, SourceURL: v.SourceURL})
	}
	return result, nil
}
