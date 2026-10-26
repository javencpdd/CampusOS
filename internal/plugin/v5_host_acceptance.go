//go:build campusos_acceptance

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// AcceptanceObserveHost observes bounded stage/error metadata only. It cannot
// authorize a request or replace its payload/result and is absent in production.
func (h *V5ManagedRuntime) AcceptanceObserveHost(observe func(string, string, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for code, endpoint := range h.hosts {
		code, original := code, endpoint
		endpoint.Authorize = func(c context.Context, owner string, s PluginV5Consume, r json.RawMessage) (string, error) {
			stamp, e := original.Authorize(c, owner, s, r)
			if e != nil {
				observe(code, fmt.Sprintf("authorize:context=%v", c.Err()), e)
			}
			return stamp, e
		}
		endpoint.Call = func(c context.Context, owner string, s PluginV5Consume, r json.RawMessage, w io.Writer) error {
			begin := time.Now()
			e := original.Call(c, owner, s, r, w)
			phase := fmt.Sprintf("call:elapsed=%s", time.Since(begin).Round(time.Millisecond))
			if c.Err() != nil {
				phase += ":" + c.Err().Error()
			}
			observe(code, phase, e)
			return e
		}
		h.hosts[code] = endpoint
	}
}
