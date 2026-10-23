package plugin

import (
	"context"
	"encoding/json"
)

// Host-only runner inputs. No HTTP handler decodes these as authority.
type V5RuntimeSpec struct {
	Program       V5WasmProgram
	Container     V5ContainerProgram
	Coordinate    V5Coordinate
	Owner         string
	Generation    int64
	ExpiresAtMS   int64
	Effective     PluginV5ManifestResources
	Configuration json.RawMessage
}
type V5RuntimeBroker func(context.Context, string, string, json.RawMessage) (json.RawMessage, error)
type V5RunningRuntime interface {
	V5ManagedAdapter
	Abort() // nonblocking cancellation; safe during admission withdrawal
	Close(context.Context) error
	Done() <-chan struct{}
}
type V5RuntimeFactory interface {
	Prepare(context.Context, V5RuntimeSpec, V5RuntimeBroker) (V5RunningRuntime, error)
}
type v5CredentialBinder interface{ BindCredential(V5RuntimeCredential) }
type v5RuntimeAborter interface{ Abort() }
type v5NestedCallKey struct{}
