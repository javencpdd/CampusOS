package campusos

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"
)

type V5ContainerSourceHostHandler func(context.Context, V5RuntimeConfiguration, V5KnowledgeRequest, V5HostTransport) (V5KnowledgeResponse, error)

// ServeV5ContainerExtensionsWithHost uses the same finite owned pipe for two
// typed contribution points. Author code still has no host credential or URL.
func ServeV5ContainerExtensionsWithHost(ctx context.Context, input io.ReadCloser, output io.Writer, chat V5ContainerChatHostHandler, source V5ContainerSourceHostHandler) error {
	return serveV5ContainerChat(ctx, input, output, nil, chat, source)
}
func v5ContainerSourceRequest(raw []byte) (q V5KnowledgeRequest, e error) {
	allowed := []string{"request_id", "contribution_id", "instance_generation", "deadline_ms", "binding_id", "cursor", "limit"}
	required := []string{"request_id", "contribution_id", "instance_generation", "deadline_ms", "binding_id", "limit"}
	if _, e = v5ContainerObject(raw, allowed, required, v5ContainerMaxJSON); e != nil {
		return q, e
	}
	if json.Unmarshal(raw, &q) != nil || !v5ContainerText(q.RequestID, 1, 128) || !v5ContainerText(q.ContributionID, 1, 128) || !v5ContainerText(q.BindingID, 1, 128) || q.InstanceGeneration < 1 || q.DeadlineMS < 1 || q.DeadlineMS > 300000 || q.Limit < 1 || q.Limit > 100 || !v5ContainerText(q.Cursor, 0, 128) {
		return V5KnowledgeRequest{}, ErrV5ContainerChatProtocol
	}
	return q, nil
}
func v5ContainerSourceInvoke(ctx context.Context, cfg V5RuntimeConfiguration, q V5KnowledgeRequest, handler func(context.Context, V5RuntimeConfiguration, V5KnowledgeRequest) (V5KnowledgeResponse, error)) (out v5ContainerFrame) {
	out = v5ContainerFrame{Contract: v5ContainerContract, Kind: "result", Failed: true}
	defer func() { _ = recover() }()
	c, cancel := context.WithTimeout(ctx, time.Duration(q.DeadlineMS)*time.Millisecond)
	defer cancel()
	r, e := handler(c, V5RuntimeConfiguration{System: bytes.Clone(cfg.System), User: bytes.Clone(cfg.User)}, q)
	if e != nil || c.Err() != nil || r.RequestID != q.RequestID || r.ContributionID != q.ContributionID || r.InstanceGeneration != q.InstanceGeneration || int64(len(r.Items)) > q.Limit || !v5ContainerText(r.NextCursor, 0, 128) {
		return out
	}
	for _, v := range r.Items {
		if !v5ContainerText(v.ExternalID, 1, 128) || !v5ContainerText(v.Revision, 1, 128) || !v5ContainerText(v.Title, 0, 500) || !v5ContainerText(v.Text, 0, 50000) || (v.SourceURL != "" && (!strings.HasPrefix(v.SourceURL, "https://") || !v5ContainerText(v.SourceURL, 1, 2048))) {
			return out
		}
	}
	if r.Items == nil {
		r.Items = []V5KnowledgeItem{}
	}
	w, e := json.Marshal(r)
	if e != nil || len(w) > v5ContainerMaxJSON || c.Err() != nil {
		return out
	}
	out.Failed = false
	out.Payload = w
	return out
}
