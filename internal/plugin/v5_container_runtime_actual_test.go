package plugin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
	container "github.com/campusos/CampusOS/internal/plugin/container"
)

func containerReference(t *testing.T) string {
	t.Helper()
	ref := os.Getenv("CAMPUSOS_V5_CONTAINER_RUNTIME_REFERENCE")
	if ref == "" {
		t.Skip("requires owned06 executable image")
	}
	if !strings.HasPrefix(ref, "localhost/campusos-owned-v12-06-runtime@sha256:") {
		t.Fatal("requires exact owned06 image repository")
	}
	return ref
}
func TestV5ContainerNativeIsolation(t *testing.T) {
	ref := containerReference(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f, err := container.NewV5Factory(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close(ctx)
	for _, transport := range []string{"http", "grpc"} {
		t.Run(transport, func(t *testing.T) {
			p := plugin.NewV5ContainerTestProgramTransport(t, ref, transport)
			spec := plugin.V5RuntimeSpec{Container: p, Coordinate: p.Coordinate(), Generation: 1, ExpiresAtMS: time.Now().Add(time.Minute).UnixMilli(), Effective: plugin.PluginV5ManifestResources{MemoryMB: 64, CPUMillis: 100, MaxConcurrency: 1, TimeoutMS: 3000, StorageBytes: 1 << 20}, Configuration: []byte(`{"system":{"message":"ordinary"}}`)}
			var brokerCalls atomic.Int64
			broker := func(ctx context.Context, token, cap string, payload json.RawMessage) (json.RawMessage, error) {
				brokerCalls.Add(1)
				if token != "parent-only" || cap != "community.thread.public.read" {
					return nil, plugin.ErrV5Denied
				}
				return []byte(`{"public":true}`), nil
			}
			prepare := func() plugin.V5RunningRuntime {
				t.Helper()
				c, stop := context.WithTimeout(ctx, 5*time.Second)
				defer stop()
				h, e := f.Prepare(c, spec, broker)
				if e != nil {
					t.Fatal("actual prepare", e)
				}
				h.(interface {
					BindCredential(plugin.V5RuntimeCredential)
				}).BindCredential(plugin.V5RuntimeCredential{Token: "parent-only"})
				return h
			}
			descriptor := plugin.V5ContributionDescriptor{InstanceGeneration: 1}
			descriptor.V5Coordinate = spec.Coordinate
			target := plugin.V5ChatTarget{Contribution: descriptor}
			invoke := func(h plugin.V5RunningRuntime, prompt string) (string, error) {
				t.Helper()
				b, _ := json.Marshal(map[string]any{"request_id": "native06", "contribution_id": "chat", "instance_generation": 1, "prompt": prompt})
				var out bytes.Buffer
				e := h.Invoke(ctx, target, b, &out)
				var reply struct{ Text string }
				_ = json.Unmarshal(out.Bytes(), &reply)
				return reply.Text, e
			}
			h := prepare()
			defer h.Close(ctx)
			t.Run("kernel-filesystem-network-environment", func(t *testing.T) {
				text, e := invoke(h, "probe")
				if e != nil {
					t.Fatal(e)
				}
				var proof struct {
					UID        int               `json:"uid"`
					Cgroup     map[string]string `json:"cgroup"`
					Socket     bool              `json:"socket_absent"`
					Readonly   bool              `json:"root_readonly"`
					Fresh      bool              `json:"fresh_directory"`
					Tmp        bool              `json:"tmp_writable"`
					Quota      bool              `json:"tmp_quota"`
					Database   bool              `json:"database_unreachable"`
					Env        []string          `json:"environment_keys"`
					Interfaces []string          `json:"interfaces"`
					Flags      map[string]string `json:"process_flags"`
				}
				if json.Unmarshal([]byte(text), &proof) != nil || proof.UID != 65534 || !proof.Socket || !proof.Readonly || !proof.Fresh || !proof.Tmp || !proof.Quota || !proof.Database || strings.Join(proof.Interfaces, ",") != "lo" {
					t.Fatal("isolation proof", text)
				}
				if proof.Cgroup["memory.max"] != "67108864" || proof.Cgroup["memory.swap.max"] != "0" || proof.Cgroup["pids.max"] != "64" {
					t.Fatal("kernel limits", text)
				}
				if proof.Flags["Seccomp"] != "2" || proof.Flags["NoNewPrivs"] != "1" || proof.Flags["CapEff"] != "0000000000000000" {
					t.Fatal("kernel privilege flags", text)
				}
				var quota, period int64
				if _, e := fmt.Sscan(proof.Cgroup["cpu.max"], &quota, &period); e != nil || quota*1000 != 100*period {
					t.Fatal("CPU control", text)
				}
				for _, key := range proof.Env {
					if key != "PATH" && key != "GOMEMLIMIT" && key != "CAMPUSOS_CONTAINER_TRANSPORT" && key != "HOME" && key != "HOSTNAME" {
						t.Fatal("inherited environment", key)
					}
				}
				t.Log("actual kernel and namespace proof", text)
			})
			t.Run("two-instances-have-private-directories", func(t *testing.T) {
				second := prepare()
				defer second.Close(ctx)
				text, e := invoke(second, "probe")
				if e != nil || !strings.Contains(text, `"fresh_directory":true`) {
					t.Fatal(text, e)
				}
			})
			t.Run("host-broker-and-unpublished-capabilities", func(t *testing.T) {
				before := brokerCalls.Load()
				if text, e := invoke(h, "ordinary"); e != nil || text != "real-container/"+transport || brokerCalls.Load() != before+1 {
					t.Fatal(text, e)
				}
				for _, cap := range []string{"secret.read", "egress.request", "unknown.call"} {
					if text, e := invoke(h, "cap:"+cap); e == nil || text != "" {
						t.Fatal("unpublished capability", cap, text, e)
					}
				}
				if brokerCalls.Load() != before+1 {
					t.Fatal("undeclared host call reached broker")
				}
			})
			t.Run("ordinary-configuration-and-event-ack", func(t *testing.T) {
				text, e := invoke(h, "configuration")
				if e != nil || !strings.Contains(text, "ordinary") {
					t.Fatal(text, e)
				}
				if e := h.Event(ctx, []byte(`{"event_id":"safe-metadata"}`)); e != nil {
					t.Fatal(e)
				}
			})
			if e := h.Close(ctx); e != nil {
				t.Fatal(e)
			}
			for _, prompt := range []string{"spin", "oom", "exit", "flood", "host-loop"} {
				t.Run(prompt, func(t *testing.T) {
					x := prepare()
					start := time.Now()
					_, e := invoke(x, prompt)
					if e == nil {
						t.Fatal("fault succeeded")
					}
					select {
					case <-x.Done():
					case <-time.After(6 * time.Second):
						t.Fatal("actual process not reaped")
					}
					if time.Since(start) > 7*time.Second {
						t.Fatal("unbounded fault")
					}
					if prompt == "oom" && x.(interface{ ExitStatus() string }).ExitStatus() != "oom_killed" {
						t.Fatal("kernel OOM not captured")
					}
				})
			}
			t.Run("caller-cancel-kills-native-process", func(t *testing.T) {
				x := prepare()
				c, stop := context.WithTimeout(ctx, 150*time.Millisecond)
				defer stop()
				e := x.Invoke(c, target, []byte(`{"prompt":"spin"}`), io.Discard)
				if e == nil {
					t.Fatal("cancel succeeded")
				}
				select {
				case <-x.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("cancel did not kill process")
				}
			})
			if len(f.Workers()) != 0 {
				t.Fatal("worker leak")
			}
		})
	}
	t.Run("orphan-fencing-preserves-other-deployment", func(t *testing.T) {
		var foreign, owned string
		fenced := false
		for _, namespace := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
			name := "campusos-container-" + strings.Repeat(namespace[:1], 32)
			out, e := v5ContainerDocker(t, ctx, "create", "--pull=never", "--name", name, "--label=campusos.container.worker=v5", "--label=campusos.container.namespace="+namespace, "--network=none", "--entrypoint=/fixture", ref).CombinedOutput()
			if e != nil {
				t.Fatal(string(out), e)
			}
			t.Cleanup(func() {
				if namespace[0] == 'a' && fenced {
					return
				}
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := v5ContainerDocker(t, cleanup, "rm", "--force", name).Run(); err != nil {
					t.Error("owned orphan cleanup", err)
				}
			})
			if namespace[0] == 'a' {
				owned = name
			} else {
				foreign = name
			}
		}
		if e := f.FenceOrphans(ctx); e != nil {
			t.Fatal(e)
		}
		if v5ContainerDocker(t, ctx, "inspect", owned).Run() == nil {
			t.Fatal("orphan survived")
		}
		if v5ContainerDocker(t, ctx, "inspect", foreign).Run() != nil {
			t.Fatal("foreign deployment removed")
		}
		fenced = true
	})
}
