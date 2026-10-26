package campusos

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

const V5KnowledgeSearch = "knowledge.public.search"
const V5KnowledgeSourceRead = "knowledge.public.source.read"

// V5HostTransport is supplied by the managed runtime, never by a bearer token
// or URL. It carries untrusted intent; all authority remains in the host.
type V5HostTransport func(context.Context, string, json.RawMessage) (json.RawMessage, error)
type V5KnowledgeSearchRequest struct {
	CollectionID string `json:"collection_id"`
	Query        string `json:"query"`
	Page         int    `json:"page"`
	PageSize     int    `json:"page_size"`
}
type V5KnowledgeSourceRequest struct {
	CollectionID string `json:"collection_id"`
	MemberID     string `json:"member_id"`
	Revision     string `json:"revision"`
}
type V5KnowledgeReference struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
}
type V5KnowledgeCitation struct {
	CollectionID       string               `json:"collection_id"`
	MemberID           string               `json:"member_id"`
	VisibilityRevision string               `json:"visibility_revision"`
	Source             V5KnowledgeReference `json:"source"`
	Generation         string               `json:"generation"`
	ChunkIndex         int                  `json:"chunk_index"`
	URL                string               `json:"url"`
}
type V5KnowledgeExplanation struct {
	Terms       []string `json:"terms"`
	TitleTerms  []string `json:"title_terms"`
	ChunkTerms  []string `json:"chunk_terms"`
	TitleWeight int      `json:"title_weight"`
	ChunkWeight int      `json:"chunk_weight"`
}
type V5KnowledgeHit struct {
	Title       string                 `json:"title"`
	Snippet     string                 `json:"snippet"`
	Score       int                    `json:"score"`
	Explanation V5KnowledgeExplanation `json:"explanation"`
	Citation    V5KnowledgeCitation    `json:"citation"`
}
type V5KnowledgeSearchPage struct {
	Items      []V5KnowledgeHit `json:"items"`
	Total      int              `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	Provider   string           `json:"provider"`
	Generation string           `json:"generation"`
}
type V5KnowledgeText struct {
	Reference  V5KnowledgeReference `json:"reference"`
	CategoryID string               `json:"category_id"`
	Title      string               `json:"title"`
	Content    string               `json:"content"`
	Format     string               `json:"format"`
	URL        string               `json:"url"`
}
type V5KnowledgeSource struct {
	CollectionID string          `json:"collection_id"`
	MemberID     string          `json:"member_id"`
	Revision     string          `json:"revision"`
	Source       V5KnowledgeText `json:"source"`
}

func v5KnowledgeRead(ctx context.Context, host V5HostTransport, code string, input, output any) error {
	if ctx == nil || ctx.Err() != nil || host == nil {
		return ErrV5ContainerChatProtocol
	}
	raw, e := json.Marshal(input)
	if e != nil {
		return ErrV5ContainerChatProtocol
	}
	wire, e := host(ctx, code, raw)
	if e != nil {
		return ErrV5ContainerChatProtocol
	}
	if _, e = v5ContainerObject(wire, nil, nil, v5ContainerMaxJSON); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(wire))
	d.DisallowUnknownFields()
	if d.Decode(output) != nil || ctx.Err() != nil {
		return ErrV5ContainerChatProtocol
	}
	return nil
}
func KnowledgeSearch(ctx context.Context, host V5HostTransport, q V5KnowledgeSearchRequest) (out V5KnowledgeSearchPage, err error) {
	err = v5KnowledgeRead(ctx, host, V5KnowledgeSearch, q, &out)
	if err != nil {
		return V5KnowledgeSearchPage{}, err
	}
	return out, nil
}
func KnowledgeSource(ctx context.Context, host V5HostTransport, q V5KnowledgeSourceRequest) (out V5KnowledgeSource, err error) {
	err = v5KnowledgeRead(ctx, host, V5KnowledgeSourceRead, q, &out)
	if err != nil {
		return V5KnowledgeSource{}, err
	}
	return out, nil
}

// Host calls are synchronous yields on the already-owned pipe. The ordinary
// chat server remains HostResult-free. This variant allows only the finite compiled
// public-read/model/answer capabilities, at most 32 yields under the original Invoke deadline.
type V5ContainerChatHostHandler func(context.Context, V5RuntimeConfiguration, V5ChatRequest, V5HostTransport) (V5ChatResponse, error)

func ServeV5ContainerChatWithHost(ctx context.Context, input io.ReadCloser, output io.Writer, handler V5ContainerChatHostHandler) error {
	if handler == nil {
		return ErrV5ContainerChatProtocol
	}
	return serveV5ContainerChat(ctx, input, output, nil, handler, nil)
}
func v5ContainerHostExchange(ctx context.Context, reader *bufio.Reader, bounded *v5ContainerReader, output io.Writer, code string, payload json.RawMessage) (json.RawMessage, error) {
	if code != V5KnowledgeSearch && code != V5KnowledgeSourceRead && code != "community.thread.public.read" && code != V5AIChatLocal && code != V5AIChatRemote && code != V5KnowledgeAnswer {
		return nil, ErrV5ContainerChatProtocol
	}
	if _, e := v5ContainerObject(payload, nil, nil, v5ContainerMaxJSON); e != nil {
		return nil, e
	}
	frame := v5ContainerFrame{Contract: v5ContainerContract, Kind: "host", Capability: code, Payload: payload}
	wire, e := json.Marshal(frame)
	if e != nil || len(wire) > v5ContainerMaxFrame || v5ContainerWrite(output, 200, wire) != nil {
		return nil, ErrV5ContainerChatProtocol
	}
	bounded.remaining = v5ContainerMaxFrame + v5ContainerMaxHead + 4096
	req, e := http.ReadRequest(reader)
	if e != nil {
		return nil, ErrV5ContainerChatProtocol
	}
	defer req.Body.Close()
	if !v5ContainerHTTPMethod(req, "HostResult", true) {
		return nil, ErrV5ContainerChatProtocol
	}
	raw, e := io.ReadAll(io.LimitReader(req.Body, v5ContainerMaxFrame+1))
	if e != nil {
		return nil, ErrV5ContainerChatProtocol
	}
	fields, e := v5ContainerObject(raw, []string{"contract", "kind", "payload", "failed"}, []string{"contract", "kind"}, v5ContainerMaxFrame)
	var contract, kind string
	var failed bool
	if e != nil || json.Unmarshal(fields["contract"], &contract) != nil || json.Unmarshal(fields["kind"], &kind) != nil || contract != v5ContainerContract || kind != "host_result" {
		return nil, ErrV5ContainerChatProtocol
	}
	if v, ok := fields["failed"]; ok && json.Unmarshal(v, &failed) != nil {
		return nil, ErrV5ContainerChatProtocol
	}
	if failed || ctx.Err() != nil {
		return nil, ErrV5ContainerChatProtocol
	}
	if _, e = v5ContainerObject(fields["payload"], nil, nil, v5ContainerMaxJSON); e != nil {
		return nil, e
	}
	return bytes.Clone(fields["payload"]), nil
}
