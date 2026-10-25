// Independently built public SDK consumer/provider. No host token or network.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	sdk "github.com/campusos/CampusOS/sdk/go"
	"os"
)

func main() {
	if sdk.ServeV5ContainerExtensionsWithHost(context.Background(), os.Stdin, os.Stdout, chat, sources) != nil {
		os.Exit(1)
	}
}
func chat(ctx context.Context, _ sdk.V5RuntimeConfiguration, q sdk.V5ChatRequest, host sdk.V5HostTransport) (sdk.V5ChatResponse, error) {
	var intent struct {
		Mode string `json:"mode"`
		sdk.V5AIChatRequest
	}
	if json.Unmarshal([]byte(q.Prompt), &intent) != nil {
		return sdk.V5ChatResponse{}, sdk.ErrV5ContainerChatProtocol
	}
	var result any
	var e error
	switch intent.Mode {
	case "model":
		result, e = sdk.ModelChat(ctx, host, intent.V5AIChatRequest)
	case "answer":
		result, e = sdk.KnowledgeAnswer(ctx, host, intent.V5AIChatRequest)
	default:
		return sdk.V5ChatResponse{}, sdk.ErrV5ContainerChatProtocol
	}
	if e != nil {
		return sdk.V5ChatResponse{}, e
	}
	wire, e := json.Marshal(result)
	if e != nil {
		return sdk.V5ChatResponse{}, e
	}
	return sdk.V5ChatResponse{RequestID: q.RequestID, ContributionID: q.ContributionID, InstanceGeneration: q.InstanceGeneration, Text: string(wire), FinishReason: "completed"}, nil
}
func sources(ctx context.Context, _ sdk.V5RuntimeConfiguration, q sdk.V5KnowledgeRequest, host sdk.V5HostTransport) (sdk.V5KnowledgeResponse, error) {
	var cursor struct {
		Query string `json:"query"`
		Page  int    `json:"page"`
	}
	raw, e := base64.RawURLEncoding.DecodeString(q.Cursor)
	if e != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Page < 1 || q.Limit > 20 {
		return sdk.V5KnowledgeResponse{}, sdk.ErrV5ContainerChatProtocol
	}
	page, e := sdk.KnowledgeSearch(ctx, host, sdk.V5KnowledgeSearchRequest{CollectionID: q.BindingID, Query: cursor.Query, Page: cursor.Page, PageSize: int(q.Limit)})
	if e != nil {
		return sdk.V5KnowledgeResponse{}, e
	}
	out := sdk.V5KnowledgeResponse{RequestID: q.RequestID, ContributionID: q.ContributionID, InstanceGeneration: q.InstanceGeneration, Items: []sdk.V5KnowledgeItem{}}
	for _, hit := range page.Items {
		source, e := sdk.KnowledgeSource(ctx, host, sdk.V5KnowledgeSourceRequest{CollectionID: q.BindingID, MemberID: hit.Citation.MemberID, Revision: hit.Citation.VisibilityRevision})
		if e != nil {
			return sdk.V5KnowledgeResponse{}, e
		}
		out.Items = append(out.Items, sdk.V5KnowledgeItem{ExternalID: source.MemberID, Revision: source.Revision, Title: source.Source.Title, Text: source.Source.Content})
	}
	if cursor.Page*int(q.Limit) < page.Total {
		cursor.Page++
		raw, _ := json.Marshal(cursor)
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
