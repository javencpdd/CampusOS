package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	campusos "github.com/campusos/CampusOS/sdk/go"
)

func TestEchoUsesOrdinaryOwnerConfiguration(t *testing.T) {
	request := campusos.V5ChatRequest{RequestID: "echo1", ContributionID: "chat-main", InstanceGeneration: 2, DeadlineMS: 3000, Prompt: "hello", ContextClass: "none"}
	for _, tc := range []struct {
		name, system, user, want string
	}{
		{"default", "", "", "Echo: hello"},
		{"system", `{"prefix":"system: "}`, "", "system: hello"},
		{"alice", `{"prefix":"system: "}`, `{"prefix":"Alice: "}`, "Alice: hello"},
		{"bob", `{"prefix":"system: "}`, `{"prefix":"Bob: "}`, "Bob: hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := echo(context.Background(), campusos.V5RuntimeConfiguration{System: json.RawMessage(tc.system), User: json.RawMessage(tc.user)}, request)
			if err != nil || out.Text != tc.want || out.RequestID != request.RequestID || out.ContributionID != request.ContributionID || out.InstanceGeneration != request.InstanceGeneration || out.FinishReason != "completed" {
				t.Fatal("owner echo mismatch", out, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := echo(ctx, campusos.V5RuntimeConfiguration{}, request); !errors.Is(err, context.Canceled) || out.Text != "" {
		t.Fatal("cancelled echo released output")
	}
	if out, err := echo(context.Background(), campusos.V5RuntimeConfiguration{System: json.RawMessage(`{"prefix":7}`)}, request); err == nil || out.Text != "" {
		t.Fatal("invalid prefix accepted")
	}
}
