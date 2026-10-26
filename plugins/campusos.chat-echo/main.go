// This independently built headless provider only echoes explicit user text.
// Current identity, ownership and authorization remain host facts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"syscall"

	campusos "github.com/campusos/CampusOS/sdk/go"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	if err := campusos.ServeV5ContainerChat(ctx, os.Stdin, os.Stdout, echo); err != nil && !errors.Is(err, context.Canceled) {
		// Do not log prompts, configuration or errors from plugin handlers.
		os.Exit(1)
	}
}

func echo(ctx context.Context, configuration campusos.V5RuntimeConfiguration, request campusos.V5ChatRequest) (campusos.V5ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return campusos.V5ChatResponse{}, err
	}
	prefix := "Echo: "
	for _, values := range []json.RawMessage{configuration.System, configuration.User} {
		if len(values) == 0 {
			continue
		}
		var current struct {
			Prefix string `json:"prefix"`
		}
		if err := json.Unmarshal(values, &current); err != nil {
			return campusos.V5ChatResponse{}, errors.New("provider.configuration_invalid")
		}
		if current.Prefix != "" {
			prefix = current.Prefix
		}
	}
	return campusos.V5ChatResponse{RequestID: request.RequestID, ContributionID: request.ContributionID, InstanceGeneration: request.InstanceGeneration, Text: prefix + request.Prompt, FinishReason: "completed"}, nil
}
