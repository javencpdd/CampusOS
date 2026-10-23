// Public SDK example. The host supplies the managed pipe and current scope;
// this program owns no platform credential, database or network endpoint.
package main

import (
	"context"
	"encoding/json"
	"os"

	sdk "github.com/campusos/CampusOS/sdk/go"
)

func main() {
	if sdk.ServeV5ContainerChatWithHost(context.Background(), os.Stdin, os.Stdout, read) != nil {
		os.Exit(1)
	}
}
func read(ctx context.Context, _ sdk.V5RuntimeConfiguration, q sdk.V5ChatRequest, host sdk.V5HostTransport) (sdk.V5ChatResponse, error) {
	var intent struct {
		Mode string `json:"mode"`
		sdk.V5KnowledgeSearchRequest
		MemberID string `json:"member_id"`
		Revision string `json:"revision"`
	}
	if json.Unmarshal([]byte(q.Prompt), &intent) != nil {
		return sdk.V5ChatResponse{}, sdk.ErrV5ContainerChatProtocol
	}
	var out any
	var err error
	switch intent.Mode {
	case "search":
		out, err = sdk.KnowledgeSearch(ctx, host, intent.V5KnowledgeSearchRequest)
	case "source":
		out, err = sdk.KnowledgeSource(ctx, host, sdk.V5KnowledgeSourceRequest{CollectionID: intent.CollectionID, MemberID: intent.MemberID, Revision: intent.Revision})
	default:
		return sdk.V5ChatResponse{}, sdk.ErrV5ContainerChatProtocol
	}
	if err != nil {
		return sdk.V5ChatResponse{}, err
	}
	wire, err := json.Marshal(out)
	if err != nil {
		return sdk.V5ChatResponse{}, err
	}
	return sdk.V5ChatResponse{RequestID: q.RequestID, ContributionID: q.ContributionID, InstanceGeneration: q.InstanceGeneration, Text: string(wire), FinishReason: "completed"}, nil
}
