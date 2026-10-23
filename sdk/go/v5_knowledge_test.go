package campusos

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestKnowledgeSDKHostYieldsUseOwnedPipeAndTypedDTOs(t *testing.T) {
	page := V5KnowledgeSearchPage{Items: []V5KnowledgeHit{{Citation: V5KnowledgeCitation{CollectionID: "collection", MemberID: "member", VisibilityRevision: "revision"}}}, Total: 1, Page: 1, PageSize: 2, Provider: "builtin.lexical/v1", Generation: "1"}
	source := V5KnowledgeSource{CollectionID: "collection", MemberID: "member", Revision: "revision", Source: V5KnowledgeText{Content: "public text"}}
	for _, failed := range []bool{false, true} {
		frames := [][]byte{v5ChatTestHTTP("Configure", v5ChatTestFrame(t, "configure", map[string]any{})), v5ChatTestHTTP("Invoke", v5ChatTestFrame(t, "invoke", v5ChatTestRequest()))}
		if failed {
			raw, _ := json.Marshal(map[string]any{"contract": v5ContainerContract, "kind": "host_result", "failed": true})
			frames = append(frames, v5ChatTestHTTP("HostResult", raw))
		} else {
			frames = append(frames, v5ChatTestHTTP("HostResult", v5ChatTestFrame(t, "host_result", page)), v5ChatTestHTTP("HostResult", v5ChatTestFrame(t, "host_result", source)))
		}
		var output bytes.Buffer
		completed := false
		err := ServeV5ContainerChatWithHost(t.Context(), io.NopCloser(bytes.NewReader(bytes.Join(frames, nil))), &output, func(ctx context.Context, _ V5RuntimeConfiguration, q V5ChatRequest, host V5HostTransport) (V5ChatResponse, error) {
			p, e := KnowledgeSearch(ctx, host, V5KnowledgeSearchRequest{CollectionID: "collection", Query: "public", Page: 1, PageSize: 2})
			if e != nil {
				return V5ChatResponse{}, e
			}
			if p.Total != 1 {
				t.Fatal("typed search")
			}
			s, e := KnowledgeSource(ctx, host, V5KnowledgeSourceRequest{CollectionID: p.Items[0].Citation.CollectionID, MemberID: p.Items[0].Citation.MemberID, Revision: p.Items[0].Citation.VisibilityRevision})
			if e != nil {
				return V5ChatResponse{}, e
			}
			if s.Revision != "revision" {
				t.Fatal("source schema")
			}
			completed = true
			r, e := v5ChatTestHandler(ctx, V5RuntimeConfiguration{}, q)
			r.Text = s.Source.Content
			return r, e
		})
		if err != nil {
			t.Fatal(err)
		}
		if completed == failed || strings.Contains(output.String(), "Authorization") || !strings.Contains(output.String(), `"capability":"knowledge.public.search"`) {
			t.Fatal("host pipe outcome", output.String())
		}
		if failed && (strings.Contains(output.String(), "public text") || !strings.Contains(output.String(), `"failed":true`)) {
			t.Fatal("failed Host result released output")
		}
	}
}
func TestKnowledgeSDKRejectsUnknownCapabilityAndUnsolicitedHostResult(t *testing.T) {
	input := append(v5ChatTestHTTP("Configure", v5ChatTestFrame(t, "configure", map[string]any{})), v5ChatTestHTTP("Invoke", v5ChatTestFrame(t, "invoke", v5ChatTestRequest()))...)
	var output bytes.Buffer
	err := ServeV5ContainerChatWithHost(t.Context(), io.NopCloser(bytes.NewReader(input)), &output, func(ctx context.Context, _ V5RuntimeConfiguration, q V5ChatRequest, host V5HostTransport) (V5ChatResponse, error) {
		_, e := host(ctx, "admin.reset", json.RawMessage(`{}`))
		return V5ChatResponse{}, e
	})
	if err != nil || strings.Contains(output.String(), `"kind":"host"`) || !strings.Contains(output.String(), `"failed":true`) {
		t.Fatal(err, output.String())
	}
	output.Reset()
	err = ServeV5ContainerChatWithHost(t.Context(), io.NopCloser(bytes.NewReader(v5ChatTestHTTP("HostResult", v5ChatTestFrame(t, "host_result", map[string]any{})))), &output, func(context.Context, V5RuntimeConfiguration, V5ChatRequest, V5HostTransport) (V5ChatResponse, error) {
		t.Fatal("unsolicited accepted")
		return V5ChatResponse{}, nil
	})
	if err == nil {
		t.Fatal("unsolicited HostResult accepted")
	}
}
