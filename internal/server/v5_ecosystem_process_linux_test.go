package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/config"
	sdk "github.com/campusos/CampusOS/sdk/go"
)

// Compiled into server.test only. The normal cmd/server has neither this
// consumer nor its private abstract Unix socket. All platform modules, HTTP
// routes, authority checks and runtime workers use the production composition.
func TestV5EcosystemHostProcess(t *testing.T) {
	if os.Getenv("CAMPUSOS_V5_ECOSYSTEM_PROCESS") != "true" {
		t.Skip("owned ecosystem subprocess only")
	}
	key, socket := os.Getenv("CAMPUSOS_V5_ECOSYSTEM_KEY"), os.Getenv("CAMPUSOS_V5_ECOSYSTEM_SOCKET")
	if len(key) < 32 || len(socket) < 16 || socket[:13] != "@campusos08a-" {
		t.Fatal("private contract socket missing")
	}
	s := New(config.Load())
	infra, err := s.startInfrastructure()
	if err != nil {
		t.Fatal(err)
	}
	defer infra.Stop()
	if infra.database == nil {
		t.Fatal("real PostgreSQL required")
	}
	stopRotation, err := startSecretRotation(infra)
	if err != nil {
		t.Fatal(err)
	}
	defer stopRotation()
	if err := s.features.Register(platformfeature.Definition{ID: "contract-chat-consumer", Mode: platformfeature.HotGated, DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	value, ok := s.appContext.Lookup("plugin.v5.dispatcher")
	hub, typed := value.(*plugin.V5ManagedRuntime)
	if !ok || !typed {
		t.Fatal("production managed dispatcher absent")
	}
	if _, ok := s.appContext.Lookup("plugin.v5.runtime-coordinator"); !ok {
		t.Fatal("production runtime coordinator absent")
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	consumer := plugin.V5Consumer{Feature: s.features, FeatureID: "contract-chat-consumer"}
	var mu sync.Mutex
	bindings := map[string]plugin.V5ManagedBinding{}
	contract := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(403)
			return
		}
		var input struct {
			Action, Token, Version, Handle, Prompt, ContextClass string
			DeadlineMS                                           int64
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		d.DisallowUnknownFields()
		if d.Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if input.Action == "feature-off" || input.Action == "feature-on" {
			if _, err := s.features.Request(consumer.FeatureID, input.Action == "feature-on"); err != nil {
				w.WriteHeader(500)
				return
			}
			w.WriteHeader(204)
			return
		}
		if input.Version != "1.1.0" && input.Version != "1.1.1" {
			w.WriteHeader(400)
			return
		}
		resolve := func(ctx context.Context) (identity.PrincipalContext, error) {
			claims, err := s.identity.JWTManager().VerifyAccessToken(input.Token)
			if err != nil {
				return identity.PrincipalContext{}, err
			}
			return identity.ResolveUserSessionPrincipal(ctx, s.identity.Sessions(), s.identity.Sessions(), claims)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		principal, err := resolve(ctx)
		if err != nil {
			w.WriteHeader(403)
			return
		}
		var binding plugin.V5ManagedBinding
		if input.Action == "cached" {
			var found bool
			binding, found = bindings[input.Handle]
			if !found {
				w.WriteHeader(404)
				return
			}
		} else if input.Action == "call" || input.Action == "remember" || input.Action == "ready" {
			binding, err = hub.Select(ctx, principal.Actor.ID, plugin.V5ContributionQuery{Point: plugin.V5AIChat, ContractVersion: "v1", Capability: "text"}, &plugin.V5ContributionRef{V5Coordinate: plugin.V5Coordinate{Publisher: "campusos", Key: "chat-echo", Version: input.Version}, ID: "chat-main"})
			if err != nil {
				t.Logf("contract selection unavailable: action=%s version=%s owner=%s code=%v", input.Action, input.Version, principal.Actor.ID, err)
				w.WriteHeader(403)
				return
			}
		} else {
			w.WriteHeader(400)
			return
		}
		if input.Action == "ready" {
			w.WriteHeader(204)
			return
		}
		request := sdk.V5ChatRequest{RequestID: "ecosystem08a", ContributionID: "chat-main", InstanceGeneration: binding.Descriptor().InstanceGeneration, DeadlineMS: input.DeadlineMS, Prompt: input.Prompt, ContextClass: input.ContextClass}
		payload, _ := json.Marshal(request)
		wire, _ := json.Marshal(sdk.V5DispatchCall{RequestID: request.RequestID, Contract: "campusos.plugin-dispatch/v1", Target: sdk.V5DispatchTarget{Kind: "contribution", ID: request.ContributionID}, InstanceGeneration: request.InstanceGeneration, DeadlineMS: time.Now().Add(4 * time.Second).UnixMilli(), RequestBytes: int64(len(payload)), ResponseLimitBytes: 65536})
		out, err := hub.Invoke(ctx, consumer, binding, wire, payload, resolve)
		if err != nil {
			t.Logf("contract invocation denied: action=%s version=%s owner=%s code=%v", input.Action, input.Version, principal.Actor.ID, err)
			if len(out) != 0 {
				t.Error("denied contract call released raw output")
			}
			w.WriteHeader(403)
			return
		}
		if input.Action == "remember" {
			bindings[input.Handle] = binding
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	})}
	defer contract.Close()
	go func() { _ = contract.Serve(listener) }()
	if err := s.runApplication(infra); err != nil {
		t.Fatal(err)
	}
}
