package server

import (
	"context"
	"encoding/json"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/ai"
	"github.com/campusos/CampusOS/internal/plugin"
	sdk "github.com/campusos/CampusOS/sdk/go"
)

type aiPluginTextAdapter struct{ owner *Server }

func (a aiPluginTextAdapter) Enabled(id string) bool {
	if id != ai.FeatureID || a.owner == nil || a.owner.cfg == nil || !a.owner.cfg.AI.Enabled || a.owner.features == nil {
		return false
	}
	state, ok := a.owner.features.Get(id)
	return ok && state.Enabled && state.DesiredEnabled
}

func (a aiPluginTextAdapter) AnswerText(ctx context.Context, resolve identityport.SessionPrincipalResolver, req ai.PluginTextRequest) (ai.PluginTextAnswer, error) {
	zero := ai.PluginTextAnswer{}
	if !a.Enabled(ai.FeatureID) || a.owner.appContext == nil || resolve == nil {
		return zero, ai.ErrManagedUnavailable
	}
	p, err := resolve(ctx)
	if err != nil || p.Validate() != nil || p.Actor.Kind != "user" {
		return zero, ai.ErrManagedDenied
	}
	v, _ := a.owner.appContext.Lookup("plugin.v5.dispatcher")
	hub, ok := v.(*plugin.V5ManagedRuntime)
	if !ok || hub == nil {
		return zero, ai.ErrManagedUnavailable
	}
	selected := &plugin.V5ContributionRef{V5Coordinate: plugin.V5Coordinate{Publisher: req.Publisher, Key: req.Key, Version: req.Version}, ID: req.ContributionID}
	binding, err := hub.Select(ctx, p.Actor.ID, plugin.V5ContributionQuery{Point: plugin.V5AIChat, ContractVersion: "v1", Capability: "text"}, selected)
	if err != nil {
		return zero, ai.ErrManagedUnavailable
	}
	d := binding.Descriptor()
	// This slice has no domain source or model Profile authorization. It can
	// forward only explicit text to a provider declaring no host private data.
	if d.DataMode != "no_host_private_data" {
		return zero, ai.ErrManagedDenied
	}
	payload, err := json.Marshal(sdk.V5ChatRequest{RequestID: req.RequestID, ContributionID: d.ID, InstanceGeneration: d.InstanceGeneration, DeadlineMS: req.TimeoutMS, Prompt: req.Prompt, ContextClass: "none"})
	if err != nil {
		return zero, ai.ErrManagedDenied
	}
	call, err := json.Marshal(sdk.V5DispatchCall{Contract: "campusos.plugin-dispatch/v1", RequestID: req.RequestID, Target: sdk.V5DispatchTarget{Kind: "contribution", ID: d.ID}, InstanceGeneration: d.InstanceGeneration, DeadlineMS: time.Now().Add(time.Duration(req.TimeoutMS) * time.Millisecond).UnixMilli(), RequestBytes: int64(len(payload)), ResponseLimitBytes: plugin.V5ChatMaxResponseBytes})
	if err != nil {
		return zero, ai.ErrManagedDenied
	}
	raw, err := hub.Invoke(ctx, plugin.V5Consumer{Feature: a, FeatureID: ai.FeatureID}, binding, call, payload, resolve)
	if err != nil {
		return zero, ai.ErrManagedUnavailable
	}
	var response sdk.V5ChatResponse
	if json.Unmarshal(raw, &response) != nil {
		return zero, ai.ErrManagedDenied
	}
	return ai.PluginTextAnswer{Text: response.Text, InputTokens: response.InputTokens, OutputTokens: response.OutputTokens, FinishReason: response.FinishReason}, nil
}
