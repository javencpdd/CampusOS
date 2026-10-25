package campusos

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAIHostSDKCapabilities(t *testing.T) {
	for _, profile := range []string{"local-chat", "remote-chat"} {
		q := V5AIChatRequest{ProfileID: profile, RunID: "ar_run", Prompt: "explicit", BindingRevision: 1, ProfileRevision: 2}
		host := func(_ context.Context, code string, raw json.RawMessage) (json.RawMessage, error) {
			if (profile == "local-chat" && code != V5AIChatLocal) || (profile == "remote-chat" && code != V5AIChatRemote) {
				t.Fatal(code)
			}
			var sent V5AIChatRequest
			if json.Unmarshal(raw, &sent) != nil || sent != q {
				t.Fatal("SDK payload")
			}
			return json.Marshal(V5AIChatAnswer{RunID: q.RunID, ProfileID: profile, Text: "answer"})
		}
		if out, e := ModelChat(t.Context(), host, q); e != nil || out.Text != "answer" {
			t.Fatal(out, e)
		}
	}
	if _, e := ModelChat(t.Context(), nil, V5AIChatRequest{ProfileID: "foreign"}); e == nil {
		t.Fatal("foreign")
	}
	if out, e := KnowledgeAnswer(t.Context(), func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return []byte(`{"status":"answered","unknown":"x"}`), nil
	}, V5AIChatRequest{}); e == nil || out.Answer != "" {
		t.Fatal("unknown fields")
	}
}
