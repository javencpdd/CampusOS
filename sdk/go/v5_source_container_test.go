package campusos

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestSourceContainerTypedProtocol(t *testing.T) {
	q := V5KnowledgeRequest{RequestID: "r", ContributionID: "source-main", InstanceGeneration: 1, DeadlineMS: 1000, BindingID: "collection", Limit: 2, Cursor: "query"}
	var input []byte
	for _, item := range []struct {
		method  string
		payload any
	}{{"Configure", V5RuntimeConfiguration{}}, {"Health", nil}, {"Invoke", q}} {
		kind := map[string]string{"Configure": "configure", "Health": "health", "Invoke": "invoke"}[item.method]
		input = append(input, v5ChatTestHTTP(item.method, v5ChatTestFrame(t, kind, item.payload))...)
	}
	var output bytes.Buffer
	e := ServeV5ContainerExtensionsWithHost(t.Context(), io.NopCloser(bytes.NewReader(input)), &output, nil, func(_ context.Context, _ V5RuntimeConfiguration, sent V5KnowledgeRequest, _ V5HostTransport) (V5KnowledgeResponse, error) {
		if sent != q {
			t.Fatal(sent)
		}
		return V5KnowledgeResponse{RequestID: q.RequestID, ContributionID: q.ContributionID, InstanceGeneration: 1, Items: []V5KnowledgeItem{{ExternalID: "member", Revision: "revision", Text: "source"}}}, nil
	})
	if e != nil || !bytes.Contains(output.Bytes(), []byte(`"external_id":"member"`)) {
		t.Fatal(e, output.String())
	}
	raw, _ := json.Marshal(q)
	if _, e := v5ContainerSourceRequest(append(raw[:len(raw)-1], []byte(`,"owner":"1001"}`)...)); e == nil {
		t.Fatal("owner accepted")
	}
	out := v5ContainerSourceInvoke(t.Context(), V5RuntimeConfiguration{}, q, func(context.Context, V5RuntimeConfiguration, V5KnowledgeRequest) (V5KnowledgeResponse, error) {
		return V5KnowledgeResponse{RequestID: q.RequestID, ContributionID: q.ContributionID, InstanceGeneration: 2}, nil
	})
	if !out.Failed || len(out.Payload) != 0 {
		t.Fatal("wrong generation")
	}
}
