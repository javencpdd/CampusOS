package plugin

import (
	"context"
	"encoding/json"
	"io"

	sdk "github.com/campusos/CampusOS/sdk/go"
)

// V5ModelMaxTimeoutMS is the absolute approved Container model-work ceiling.
const V5ModelMaxTimeoutMS = 30000

const V5AIChatLocal = "ai.chat.local.invoke"
const V5AIChatRemote = "ai.chat.remote.invoke"
const V5KnowledgeAnswer = "knowledge.public.answer"

func V5AIProfile(code string) string {
	if code == V5AIChatLocal {
		return "local-chat"
	}
	if code == V5AIChatRemote {
		return "remote-chat"
	}
	return ""
}

func ParseV5AIRequest(raw json.RawMessage, rag bool) (sdk.V5AIChatRequest, error) {
	var out sdk.V5AIChatRequest
	v, ok := v5JSON(raw)
	fields := map[string]v5Check{"profile_id": v5Enum("local-chat", "remote-chat"), "binding_revision": v5String(1, 19, `^[1-9][0-9]*$`), "profile_revision": v5String(1, 19, `^[1-9][0-9]*$`), "run_id": v5String(35, 35, `^ar_[0-9a-f]{32}$`), "prompt": v5String(1, 12000, `(?s)^.*$`)}
	required := "profile_id binding_revision profile_revision run_id prompt"
	if rag {
		fields["collection_id"] = v5String(35, 35, `^kc_[0-9a-f]{32}$`)
		required += " collection_id"
	}
	if !ok || !v5Object(required, fields)(v) {
		return out, ErrV5CallInvalid
	}
	if json.Unmarshal(raw, &out) != nil || out.BindingRevision < 1 || out.ProfileRevision < 1 {
		return sdk.V5AIChatRequest{}, ErrV5CallInvalid
	}
	return out, nil
}

// Side-effecting endpoints authorize only current facts. Authorize is never an
// invocation/retry and runs both before dispatch and after buffered settlement.
func V5ModelEndpoint(code string, feature V5ChatFeatureReader, authorize func(context.Context, string, PluginV5Consume, json.RawMessage) (string, error), call func(context.Context, string, PluginV5Consume, json.RawMessage, io.Writer) error) V5HostEndpoint {
	return V5HostEndpoint{Feature: feature, FeatureID: "ai-gateway", Authorize: authorize, Call: call}
}
