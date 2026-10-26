package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/features/ai"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/plugin"
	sdk "github.com/campusos/CampusOS/sdk/go"
)

type pluginRAGGate struct{ owner *Server }

func (g pluginRAGGate) Enabled(string) bool {
	return aiPluginTextAdapter{owner: g.owner}.Enabled(ai.FeatureID) && g.owner.knowledge != nil && g.owner.features.Enabled(knowledge.FeatureID)
}

func modelCapability(profile string) string {
	if profile == "local-chat" {
		return plugin.V5AIChatLocal
	}
	if profile == "remote-chat" {
		return plugin.V5AIChatRemote
	}
	return ""
}

func (m *pluginPlatformModule) modelHostPlan(ctx context.Context, owner string, q sdk.V5AIChatRequest) (plugin.V5InvocationAuthority, ai.LocalDisclosurePlan, error) {
	var zero ai.LocalDisclosurePlan
	authority, e := plugin.V5CurrentInvocation(ctx, owner)
	if e != nil {
		return authority, zero, fmt.Errorf("%w: invocation context=%v", plugin.ErrV5Denied, ctx.Err())
	}
	scope, e := authority.Require(ctx, modelCapability(q.ProfileID))
	if e != nil || scope.Scope.Kind != "self" {
		return authority, zero, fmt.Errorf("%w: capability context=%v", plugin.ErrV5Denied, ctx.Err())
	}
	if !(aiPluginTextAdapter{owner: m.owner}).Enabled(ai.FeatureID) {
		return authority, zero, plugin.ErrV5Denied
	}
	if m.owner.ai == nil || m.owner.ai.Managed() == nil {
		return authority, zero, plugin.ErrV5Denied
	}
	plan, e := m.owner.ai.Managed().PlanChatDisclosure(ctx, authority.Resolve, q.BindingRevision)
	if e != nil || plan.ProfileID != q.ProfileID || plan.ProfileRevision != q.ProfileRevision || plan.BindingRevision != q.BindingRevision {
		return authority, zero, fmt.Errorf("%w: plan context=%v", plugin.ErrV5Denied, ctx.Err())
	}
	return authority, plan, nil
}
func (m *pluginPlatformModule) auditModelHost(ctx context.Context, owner string, q sdk.V5AIChatRequest, authority plugin.V5InvocationAuthority) error {
	value, _ := m.app.Lookup("platform.reliability.service")
	service, ok := value.(*reliability.Service)
	if !ok || service == nil {
		return plugin.ErrV5Denied
	}
	return service.Execute(ctx, reliability.Command{Code: "plugin.ai.authorize", ActorType: "user", ActorID: owner, ResourceType: "plugin.ai", ResourceID: q.RunID, OperationCode: "plugin.ai.authorize", EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("plugin.ai.authorized.v1", "plugin.ai", q.RunID, map[string]string{"publisher": authority.Coordinate.Publisher, "key": authority.Coordinate.Key, "version": authority.Coordinate.Version, "generation": strconv.FormatInt(authority.Generation, 10), "profile_id": q.ProfileID, "run_id": q.RunID})
	}}, func(tx context.Context) error { _, _, e := m.modelHostPlan(tx, owner, q); return e })
}
func (m *pluginPlatformModule) bindAIHost() error {
	authorize := func(code string, rag bool) func(context.Context, string, plugin.PluginV5Consume, json.RawMessage) (string, error) {
		return func(ctx context.Context, owner string, scope plugin.PluginV5Consume, raw json.RawMessage) (string, error) {
			q, e := plugin.ParseV5AIRequest(raw, rag)
			if e != nil {
				return "", e
			}
			if scope.Capability != code {
				return "", plugin.ErrV5Denied
			}
			if !rag && (scope.Scope.Kind != "self" || plugin.V5AIProfile(code) != q.ProfileID) {
				return "", plugin.ErrV5Denied
			}
			_, plan, e := m.modelHostPlan(ctx, owner, q)
			if e != nil {
				return "", e
			}
			var collection any
			if rag {
				if scope.Scope.Kind != "collection" || !containsCollection(scope.Scope.CollectionIDs, q.CollectionID) || m.owner.knowledge == nil {
					return "", plugin.ErrV5Denied
				}
				state, e := m.owner.knowledge.Collections().PublicBindingRevision(ctx, q.CollectionID)
				if e != nil {
					return "", fmt.Errorf("%w: collection context=%v", plugin.ErrV5Denied, ctx.Err())
				}
				collection = state
			}
			wire, e := json.Marshal([]any{plan, collection})
			if e != nil {
				return "", plugin.ErrV5Denied
			}
			sum := sha256.Sum256(wire)
			return hex.EncodeToString(sum[:]), nil
		}
	}
	for _, code := range []string{plugin.V5AIChatLocal, plugin.V5AIChatRemote, plugin.V5KnowledgeAnswer} {
		code := code
		rag := code == plugin.V5KnowledgeAnswer
		auth := authorize(code, rag)
		call := func(ctx context.Context, owner string, scope plugin.PluginV5Consume, raw json.RawMessage, w io.Writer) error {
			q, e := plugin.ParseV5AIRequest(raw, rag)
			if e != nil {
				return e
			}
			stamp, e := auth(ctx, owner, scope, raw)
			if e != nil {
				return e
			}
			authority, _, e := m.modelHostPlan(ctx, owner, q)
			if e != nil {
				return e
			}
			if e = m.auditModelHost(ctx, owner, q, authority); e != nil {
				return e
			}
			guard := func(c context.Context) error {
				current, e := auth(c, owner, scope, raw)
				if e != nil {
					return fmt.Errorf("%w: model_scope_read context=%v", plugin.ErrV5Denied, c.Err())
				}
				if current != stamp {
					return fmt.Errorf("%w: model_scope_revision context=%v", plugin.ErrV5Denied, c.Err())
				}
				return nil
			}
			var output any
			if rag {
				if m.owner.knowledge.PublicRAG() == nil {
					return plugin.ErrV5Denied
				}
				output, e = m.owner.knowledge.PublicRAG().AnswerChecked(ctx, authority.Resolve, knowledge.PublicLocalQuestion{CollectionID: q.CollectionID, Question: q.Prompt, RunID: q.RunID, BindingRevision: q.BindingRevision}, guard)
			} else {
				result, err := m.owner.ai.Managed().InvokeChecked(ctx, authority.Resolve, q.RunID, q.BindingRevision, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: q.Prompt}}}, guard)
				e = err
				if e == nil {
					output = sdk.V5AIChatAnswer{RunID: result.Run.ID, Text: result.Response.Content, ProfileID: q.ProfileID}
				}
			}
			if e != nil {
				return e
			}
			if e = guard(ctx); e != nil {
				return e
			}
			wire, e := json.Marshal(output)
			if e != nil {
				return e
			}
			_, e = w.Write(wire)
			return e
		}
		endpoint := plugin.V5ModelEndpoint(code, aiPluginTextAdapter{owner: m.owner}, auth, call)
		if rag {
			endpoint.Feature = pluginRAGGate{owner: m.owner}
		}
		if e := m.managed.BindHost(code, endpoint); e != nil {
			return e
		}
	}
	return nil
}
func containsCollection(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
