package wasm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
)

// A fixed host-built helper image, never a plugin-supplied container artifact.
// Local immutable image IDs only: no pull, mutable tag, socket or host mount.
// A single elected host reserves whole cgroup budgets before creating workers.
type V5IsolatedFactory struct {
	workers     map[string]*v5IsolatedRunning
	image       string
	namespace   string
	mu          sync.Mutex
	memory, cpu int64
	instances   int
}

func NewV5IsolatedFactory(image string) (*V5IsolatedFactory, error) {
	if runtime.GOOS != "linux" || !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(image) {
		return nil, plugin.ErrV5RuntimeMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := workerCommand(ctx, "image", "inspect", "--format={{.Id}}", image).Output()
	resolved, resolveErr := resolveV5WorkerImage(out)
	if err != nil || resolveErr != nil {
		return nil, plugin.ErrV5RuntimeMissing
	}
	var scope [32]byte
	if _, err := rand.Read(scope[:]); err != nil {
		return nil, plugin.ErrV5RuntimeMissing
	}
	return &V5IsolatedFactory{image: resolved, namespace: hex.EncodeToString(scope[:])}, nil
}

func NewV5IsolatedFactoryForDeployment(image, namespace string) (*V5IsolatedFactory, error) {
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(namespace) {
		return nil, plugin.ErrV5RuntimeMissing
	}
	f, err := NewV5IsolatedFactory(image)
	if err != nil {
		return nil, err
	}
	f.namespace = namespace
	return f, nil
}

// Docker build can return a manifest/index digest while image inspect returns
// the platform image configuration ID. Pin launches and inspect checks to that
// resolved immutable ID; never compare these different digest namespaces.
func resolveV5WorkerImage(raw []byte) (string, error) {
	id := strings.TrimSpace(string(raw))
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(id) {
		return "", plugin.ErrV5RuntimeMissing
	}
	return id, nil
}

func workerCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/usr/bin/docker", args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin"} // no application environment or credentials
	return cmd
}

func removeV5Worker(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := workerCommand(ctx, "rm", "--force", name).Run(); err == nil {
		return nil
	}
	raw, err := workerCommand(ctx, "ps", "--all", "--filter=name=^/"+name+"$", "--format={{.Names}}").Output()
	if err == nil && strings.TrimSpace(string(raw)) == "" {
		return nil
	}
	return plugin.ErrV5RuntimeMissing
}

func (f *V5IsolatedFactory) Prepare(ctx context.Context, spec plugin.V5RuntimeSpec, broker plugin.V5RuntimeBroker) (plugin.V5RunningRuntime, error) {
	r := spec.Effective
	if ctx.Err() != nil || spec.Program.Digest() == "" || r.MemoryMB < 1 || r.MemoryMB > 256 || r.CPUMillis < 1 || r.CPUMillis > 1000 || r.TimeoutMS < 1 || r.TimeoutMS > 5000 || r.MaxConcurrency != 1 || broker == nil {
		return nil, ErrV5SandboxBudget
	}
	f.mu.Lock()
	if f.instances >= 16 || f.memory+r.MemoryMB > 1024 || f.cpu+r.CPUMillis > 4000 {
		f.mu.Unlock()
		return nil, ErrV5SandboxBudget
	}
	f.instances++
	f.memory += r.MemoryMB
	f.cpu += r.CPUMillis
	f.mu.Unlock()
	release := func() { f.mu.Lock(); defer f.mu.Unlock(); f.instances--; f.memory -= r.MemoryMB; f.cpu -= r.CPUMillis }
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		release()
		return nil, ErrV5SandboxCall
	}
	name := "campusos-wasm-" + hex.EncodeToString(nonce[:])
	args := []string{"create", "--interactive", "--label=campusos.wasm.worker=v5", "--label=campusos.wasm.namespace=" + f.namespace, "--label=campusos.wasm.coordinate=" + spec.Coordinate.Publisher + "/" + spec.Coordinate.Key + "/" + spec.Coordinate.Version, "--log-driver=none", "--name", name, "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=" + strconv.FormatInt(r.MemoryMB<<20, 10), "--memory-swap=" + strconv.FormatInt(r.MemoryMB<<20, 10), "--cpus=" + strconv.FormatFloat(float64(r.CPUMillis)/1000, 'f', 3, 64), "--user=65534:65534", "--ipc=none", "--ulimit=nofile=64:64", "--env=PATH=/", "--env=GOMEMLIMIT=" + strconv.FormatInt((r.MemoryMB<<20)*3/4, 10), f.image}
	if err := workerCommand(ctx, args...).Run(); err != nil {
		release()
		removeV5Worker(name)
		return nil, plugin.ErrV5RuntimeMissing
	}
	life, cancel := context.WithCancel(context.Background())
	cmd := workerCommand(life, "start", "--attach", "--interactive", name)
	cmd.Cancel = func() error {
		// Keep the stopped container until Wait has captured the kernel's OOM
		// result. Removing it here races EOF handling and loses that evidence.
		stopCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		_ = workerCommand(stopCtx, "kill", name).Run()
		stop()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		removeV5Worker(name)
		release()
		return nil, ErrV5SandboxCall
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		in.Close()
		removeV5Worker(name)
		release()
		return nil, ErrV5SandboxCall
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		in.Close()
		out.Close()
		removeV5Worker(name)
		release()
		return nil, plugin.ErrV5RuntimeMissing
	}
	h := &v5IsolatedRunning{spec: spec, broker: broker, stdin: in, stdout: out, cancel: cancel, done: make(chan struct{}), name: name, image: f.image}
	f.mu.Lock()
	if f.workers == nil {
		f.workers = map[string]*v5IsolatedRunning{}
	}
	f.workers[name] = h
	f.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		stateCtx, stateCancel := context.WithTimeout(context.Background(), time.Second)
		raw, _ := workerCommand(stateCtx, "inspect", "--format={{.State.OOMKilled}}", name).Output()
		stateCancel()
		if strings.TrimSpace(string(raw)) == "true" {
			h.exit.Store("oom_killed")
		} else {
			h.exit.Store("exited")
		}
		cancel()
		in.Close()
		out.Close()
		for removeV5Worker(name) != nil {
			time.Sleep(time.Second)
		}
		f.mu.Lock()
		delete(f.workers, name)
		f.mu.Unlock()
		release()
		close(h.done)
	}()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	declared := []string{}
	events := false
	for _, c := range spec.Program.Consumes() {
		declared = append(declared, c.Capability)
		events = events || strings.HasSuffix(c.Capability, ".subscribe")
	}
	init := v5WorkerFrame{Kind: "init", Code: spec.Program.Bytes(), Limits: V5SandboxLimits{r.MemoryMB, 1, r.TimeoutMS}, ExpiresAtMS: spec.ExpiresAtMS, Declared: declared, EventRequired: events, Configuration: spec.Configuration}
	err = writeV5Frame(in, init, v5InitLimit)
	if err == nil {
		var ready v5WorkerFrame
		ready, err = readV5Frame(out, v5FrameLimit)
		if ready.Kind != "ready" {
			err = ErrV5SandboxCall
		}
		if err == nil {
			err = h.checkControls(ctx, ready.Controls)
		}
	}
	if err != nil || ctx.Err() != nil {
		h.Abort()
		closeCtx, stop := context.WithTimeout(context.Background(), 4*time.Second)
		defer stop()
		_ = h.Close(closeCtx)
		return nil, plugin.ErrV5RuntimeMissing
	}
	return h, nil
}

type v5IsolatedRunning struct {
	proof       atomic.Value
	exit        atomic.Value
	spec        plugin.V5RuntimeSpec
	broker      plugin.V5RuntimeBroker
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	cancel      context.CancelFunc
	done        chan struct{}
	busy        atomic.Bool
	credential  atomic.Value
	name, image string
}

func (h *v5IsolatedRunning) BindCredential(c plugin.V5RuntimeCredential) { h.credential.Store(c.Token) }
func (h *v5IsolatedRunning) Done() <-chan struct{}                       { return h.done }
func (h *v5IsolatedRunning) Abort()                                      { h.cancel() }
func (h *v5IsolatedRunning) Close(ctx context.Context) error {
	h.Abort()
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (h *v5IsolatedRunning) Invoke(ctx context.Context, t plugin.V5ChatTarget, raw json.RawMessage, w io.Writer) error {
	d := t.Contribution
	if d.V5Coordinate != h.spec.Coordinate || t.Owner != h.spec.Owner || d.InstanceGeneration != h.spec.Generation {
		return plugin.ErrV5ContributionStale
	}
	out, err := h.call(ctx, "invoke", raw)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}
func (h *v5IsolatedRunning) Event(ctx context.Context, raw json.RawMessage) error {
	_, err := h.call(ctx, "event", raw)
	return err
}
func (h *v5IsolatedRunning) call(ctx context.Context, kind string, raw json.RawMessage) (json.RawMessage, error) {
	if !h.busy.CompareAndSwap(false, true) {
		return nil, ErrV5SandboxBudget
	}
	defer h.busy.Store(false)
	select {
	case <-h.done:
		return nil, ErrV5SandboxClosed
	default:
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	deadline := time.Now().Add(time.Duration(h.spec.Effective.TimeoutMS) * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	stop := context.AfterFunc(callCtx, h.Abort)
	defer stop()
	if len(raw) > V5SandboxMaxJSONBytes {
		return nil, ErrV5SandboxBudget
	}
	if err := writeV5Frame(h.stdin, v5WorkerFrame{Kind: kind, Payload: raw, DeadlineMS: deadline.UnixMilli()}, v5FrameLimit); err != nil {
		h.Abort()
		return nil, ErrV5SandboxCall
	}
	for count := 0; count <= 32; count++ {
		reply, err := readV5Frame(h.stdout, v5FrameLimit)
		if err != nil || callCtx.Err() != nil {
			h.Abort()
			return nil, ErrV5SandboxCall
		}
		if reply.Kind == "result" {
			if reply.Failed {
				return nil, ErrV5SandboxCall
			}
			if len(reply.Payload) > V5SandboxMaxJSONBytes {
				return nil, ErrV5SandboxBudget
			}
			return reply.Payload, nil
		}
		if reply.Kind != "host" || count >= 32 || kind == "ping" {
			h.Abort()
			return nil, ErrV5SandboxABI
		}
		token, _ := h.credential.Load().(string)
		out, err := h.broker(callCtx, token, reply.Capability, reply.Payload)
		if len(out) > V5SandboxMaxJSONBytes {
			out = nil
			err = ErrV5SandboxBudget
		}
		if err := writeV5Frame(h.stdin, v5WorkerFrame{Kind: "host_result", Payload: out, Failed: err != nil}, v5FrameLimit); err != nil {
			h.Abort()
			return nil, ErrV5SandboxCall
		}
	}
	h.Abort()
	return nil, ErrV5SandboxBudget
}

// CLI acceptance is insufficient: verify Docker configuration AND the actual
// cgroup files read by the trusted worker in its private cgroup namespace.
func (h *v5IsolatedRunning) checkControls(ctx context.Context, controls map[string]string) error {
	raw, err := workerCommand(ctx, "inspect", h.name).Output()
	if err != nil {
		return ErrV5SandboxCall
	}
	var rows []struct {
		Image  string
		Mounts []any
		Config struct {
			User string
			Env  []string
		}
		HostConfig struct {
			Privileged     bool
			ReadonlyRootfs bool
			NetworkMode    string
			CapAdd         []string
			CapDrop        []string
			SecurityOpt    []string
			Memory         int64
			MemorySwap     int64
			NanoCpus       int64
			PidsLimit      int64
			Binds          []string
			PidMode        string
			IpcMode        string
			LogConfig      struct{ Type string }
		}
	}
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 1 {
		return ErrV5SandboxCall
	}
	x := rows[0]
	r := h.spec.Effective
	if x.Image != h.image || len(x.Mounts) != 0 || x.Config.User != "65534:65534" || x.HostConfig.Privileged || !x.HostConfig.ReadonlyRootfs || x.HostConfig.NetworkMode != "none" || len(x.HostConfig.CapAdd) != 0 || len(x.HostConfig.Binds) != 0 || x.HostConfig.PidMode != "" || x.HostConfig.IpcMode != "none" || x.HostConfig.LogConfig.Type != "none" || x.HostConfig.Memory != r.MemoryMB<<20 || x.HostConfig.MemorySwap != r.MemoryMB<<20 || x.HostConfig.NanoCpus != r.CPUMillis*1000000 || x.HostConfig.PidsLimit != 64 {
		return ErrV5SandboxCall
	}
	if len(x.HostConfig.CapDrop) != 1 || strings.ToUpper(x.HostConfig.CapDrop[0]) != "ALL" || len(x.HostConfig.SecurityOpt) != 1 || !strings.HasPrefix(x.HostConfig.SecurityOpt[0], "no-new-privileges") {
		return ErrV5SandboxCall
	}
	if len(x.Config.Env) != 2 || !strings.HasPrefix(x.Config.Env[0], "GOMEMLIMIT=") && !strings.HasPrefix(x.Config.Env[1], "GOMEMLIMIT=") || !(x.Config.Env[0] == "PATH=/" || x.Config.Env[1] == "PATH=/") {
		return ErrV5SandboxCall
	}
	if controls["memory.max"] != strconv.FormatInt(r.MemoryMB<<20, 10) || controls["memory.swap.max"] != "0" || controls["pids.max"] != "64" {
		return ErrV5SandboxCall
	}
	var quota, period int64
	if _, err := fmt.Sscan(controls["cpu.max"], &quota, &period); err != nil || quota < 1 || period < 1 || quota*1000 > r.CPUMillis*period {
		return ErrV5SandboxCall
	}
	h.proof.Store(controls)
	return nil
}

// Proof contains only finite kernel controls, never a request, token or path.
func (h *v5IsolatedRunning) IsolationProof() map[string]string {
	out := map[string]string{}
	proof, _ := h.proof.Load().(map[string]string)
	for k, v := range proof {
		out[k] = v
	}
	return out
}

// Private control-plane diagnostics; not registered as an HTTP/plugin API.
type V5WorkerInfo struct {
	Coordinate plugin.V5Coordinate
	Owner      string
	Generation int64
	Name       string
	Controls   map[string]string
}

func (f *V5IsolatedFactory) Workers() []V5WorkerInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []V5WorkerInfo{}
	for name, h := range f.workers {
		out = append(out, V5WorkerInfo{h.spec.Coordinate, h.spec.Owner, h.spec.Generation, name, h.IsolationProof()})
	}
	return out
}
func (h *v5IsolatedRunning) ExitStatus() string {
	if value, ok := h.exit.Load().(string); ok {
		return value
	}
	return "running"
}
func (h *v5IsolatedRunning) Health(ctx context.Context) error {
	if h.busy.Load() {
		return nil
	}
	_, err := h.call(ctx, "ping", nil)
	if err == ErrV5SandboxBudget {
		return nil
	}
	return err
}

// FenceOrphans is called only after acquiring the exclusive database guard.
// These labels and the random name prefix are reserved for this host launcher;
// guests cannot access Docker. Reap old generations before reserving new CPU/RSS.
func (f *V5IsolatedFactory) FenceOrphans(ctx context.Context) error {
	raw, err := workerCommand(ctx, "ps", "--all", "--filter=label=campusos.wasm.worker=v5", "--filter=label=campusos.wasm.namespace="+f.namespace, "--format={{.Names}}").Output()
	if err != nil {
		return plugin.ErrV5RuntimeMissing
	}
	for _, name := range strings.Fields(string(raw)) {
		if !regexp.MustCompile(`^campusos-wasm-[0-9a-f]{32}$`).MatchString(name) {
			return plugin.ErrV5RuntimeMissing
		}
		if err := workerCommand(ctx, "rm", "--force", name).Run(); err != nil {
			return plugin.ErrV5RuntimeMissing
		}
	}
	return nil
}
