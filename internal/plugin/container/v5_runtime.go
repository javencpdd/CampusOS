package container

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/plugin/containerwire"
)

var ownedName = regexp.MustCompile(`^campusos-container-[0-9a-f]{32}$`)
var namespacePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var errContainerBusy = errors.New("plugin.container_busy")

type V5Factory struct {
	home, namespace string
	mu              sync.Mutex
	workers         map[string]*v5Running
	closed          bool
	memory, cpu     int64
	instances       int
	preparing       sync.WaitGroup
	life            context.Context
	cancel          context.CancelFunc
}

func NewV5Factory(namespace string) (*V5Factory, error) {
	if !namespacePattern.MatchString(namespace) || runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return nil, ErrImagePlatform
	}
	home, err := os.MkdirTemp("", "campusos-container-cli-")
	if err != nil {
		return nil, ErrImageUnavailable
	}
	f := &V5Factory{home: home, namespace: namespace, workers: map[string]*v5Running{}}
	f.life, f.cancel = context.WithCancel(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := f.output(ctx, "info", "--format={{.OSType}}/{{.CgroupVersion}}\n{{json .SecurityOptions}}\n{{.MemoryLimit}}/{{.SwapLimit}}/{{.CPUCfsQuota}}/{{.CPUCfsPeriod}}/{{.PidsLimit}}")
	parts := strings.SplitN(strings.TrimSpace(string(raw)), "\n", 3)
	var security []string
	if err != nil || len(parts) != 3 || parts[0] != "linux/2" || json.Unmarshal([]byte(parts[1]), &security) != nil || !slices.Contains(security, "name=seccomp,profile=builtin") || parts[2] != "true/true/true/true/true" {
		os.RemoveAll(home)
		f.cancel()
		return nil, ErrImagePlatform
	}
	return f, nil
}
func (f *V5Factory) command(ctx context.Context, args ...string) *exec.Cmd {
	return localCommand(ctx, f.home, args...)
}
func (f *V5Factory) output(ctx context.Context, args ...string) ([]byte, error) {
	var out boundedImageOutput
	c := f.command(ctx, args...)
	c.Stdout = &out
	err := c.Run()
	if out.overflow {
		return nil, ErrImageIdentity
	}
	return out.Bytes(), err
}
func (f *V5Factory) remove(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := f.command(ctx, "rm", "--force", name).Run(); err == nil {
		return nil
	}
	// A concurrent trusted cleanup may already have removed it. A failed
	// daemon query is not proof of absence and cannot release a reservation.
	raw, err := f.output(ctx, "ps", "--all", "--filter=name=^/"+name+"$", "--format={{.Names}}")
	if err == nil && strings.TrimSpace(string(raw)) == "" {
		return nil
	}
	return ErrImageUnavailable
}

func (f *V5Factory) Prepare(ctx context.Context, s plugin.V5RuntimeSpec, b plugin.V5RuntimeBroker) (plugin.V5RunningRuntime, error) {
	f.mu.Lock()
	if f.closed || f.life == nil {
		f.mu.Unlock()
		return nil, ErrImageUnavailable
	}
	f.preparing.Add(1)
	f.mu.Unlock()
	defer f.preparing.Done()
	ctx, cancelPreparation := context.WithCancel(ctx)
	defer cancelPreparation()
	stopPreparation := context.AfterFunc(f.life, cancelPreparation)
	defer stopPreparation()
	r := s.Effective
	if ctx.Err() != nil || b == nil || s.Program.Digest() != "" || s.Container.PackageDigest() == "" || s.Container.Coordinate() != s.Coordinate || s.Generation < 1 || s.ExpiresAtMS <= time.Now().UnixMilli() || r.MemoryMB < 1 || r.MemoryMB > 256 || r.CPUMillis < 1 || r.CPUMillis > 1000 || r.MaxConcurrency != 1 || r.TimeoutMS < 1 || r.TimeoutMS > plugin.V5ModelMaxTimeoutMS || r.StorageBytes < 1 || r.StorageBytes > 1<<20 || len(r.NetworkTargets) != 0 || len(s.Configuration) > 65536 || !json.Valid(s.Configuration) {
		return nil, plugin.ErrV5RuntimeMissing
	}
	image, err := ResolveLocalImage(ctx, s.Container)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	if f.closed || f.instances >= 16 || f.memory+r.MemoryMB > 1024 || f.cpu+r.CPUMillis > 4000 {
		f.mu.Unlock()
		return nil, plugin.ErrV5RuntimeMissing
	}
	// Reserve while daemon IO is in flight, before the worker becomes visible.
	f.memory += r.MemoryMB
	f.cpu += r.CPUMillis
	f.instances++
	f.mu.Unlock()
	release := func() { f.mu.Lock(); defer f.mu.Unlock(); f.memory -= r.MemoryMB; f.cpu -= r.CPUMillis; f.instances-- }
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		release()
		return nil, ErrImageUnavailable
	}
	name := "campusos-container-" + hex.EncodeToString(nonce[:])
	args := []string{"create", "--interactive", "--pull=never", "--name", name, "--label=campusos.container.worker=v5", "--label=campusos.container.namespace=" + f.namespace, "--label=campusos.container.coordinate=" + s.Coordinate.Publisher + "/" + s.Coordinate.Key + "/" + s.Coordinate.Version, "--log-driver=none", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=" + strconv.FormatInt(r.MemoryMB<<20, 10), "--memory-swap=" + strconv.FormatInt(r.MemoryMB<<20, 10), "--cpus=" + strconv.FormatFloat(float64(r.CPUMillis)/1000, 'f', 3, 64), "--user=65534:65534", "--ipc=none", "--cgroupns=private", "--ulimit=nofile=64:64", "--no-healthcheck", "--workdir=/tmp", "--tmpfs=/tmp:rw,noexec,nosuid,nodev,size=" + strconv.FormatInt(r.StorageBytes, 10) + ",mode=1777", "--env=CAMPUSOS_CONTAINER_TRANSPORT=" + s.Container.Transport(), "--env=GOMEMLIMIT=" + strconv.FormatInt((r.MemoryMB<<20)*3/4, 10), image.ID()}
	if err := f.command(ctx, args...).Run(); err != nil {
		f.remove(name)
		release()
		return nil, ErrImageUnavailable
	}
	life, cancel := context.WithCancel(context.Background())
	cmd := f.command(life, "start", "--attach", "--interactive", name)
	cmd.Cancel = func() error {
		stopCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		_ = f.command(stopCtx, "kill", name).Run()
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
		f.remove(name)
		release()
		return nil, ErrImageUnavailable
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		in.Close()
		f.remove(name)
		release()
		return nil, ErrImageUnavailable
	}
	if err := cmd.Start(); err != nil {
		cancel()
		in.Close()
		out.Close()
		f.remove(name)
		release()
		return nil, ErrImageUnavailable
	}
	h := &v5Running{factory: f, spec: s, broker: b, cancel: cancel, done: make(chan struct{}), name: name, image: image.ID()}
	h.pipe = containerwire.NewPipe(out, in, h.Abort)
	f.mu.Lock()
	f.workers[name] = h
	closed := f.closed
	f.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		stopCtx, stop := context.WithTimeout(context.Background(), time.Second)
		raw, _ := f.output(stopCtx, "inspect", "--format={{.State.OOMKilled}}", name)
		stop()
		if strings.TrimSpace(string(raw)) == "true" {
			h.exit.Store("oom_killed")
		} else {
			h.exit.Store("exited")
		}
		cancel()
		h.pipe.Close()
		for f.remove(name) != nil {
			time.Sleep(time.Second)
		}
		f.mu.Lock()
		delete(f.workers, name)
		f.mu.Unlock()
		release()
		close(h.done)
	}()
	stop := context.AfterFunc(ctx, h.Abort)
	defer stop()
	if closed {
		h.Abort()
		return nil, ErrImageUnavailable
	}
	h.client, err = containerwire.NewClient(h.pipe, s.Container.Transport())
	if err == nil {
		_, err = h.call(ctx, "Configure", "configure", s.Configuration)
	}
	if err == nil {
		err = h.checkControls(ctx)
	}
	if err != nil || ctx.Err() != nil {
		h.Abort()
		stopCtx, stop := context.WithTimeout(context.Background(), 4*time.Second)
		defer stop()
		_ = h.Close(stopCtx)
		return nil, plugin.ErrV5RuntimeMissing
	}
	return h, nil
}

type v5Running struct {
	factory     *V5Factory
	spec        plugin.V5RuntimeSpec
	broker      plugin.V5RuntimeBroker
	cancel      context.CancelFunc
	done        chan struct{}
	pipe        *containerwire.PipeConn
	client      *containerwire.Client
	busy        atomic.Bool
	credential  atomic.Value
	exit        atomic.Value
	name, image string
}

func (h *v5Running) BindCredential(c plugin.V5RuntimeCredential) { h.credential.Store(c.Token) }
func (h *v5Running) Done() <-chan struct{}                       { return h.done }
func (h *v5Running) Abort()                                      { h.cancel() }
func (h *v5Running) Close(ctx context.Context) error {
	h.Abort()
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (h *v5Running) ExitStatus() string {
	if s, ok := h.exit.Load().(string); ok {
		return s
	}
	return "running"
}
func (h *v5Running) Invoke(ctx context.Context, t plugin.V5ChatTarget, raw json.RawMessage, w io.Writer) error {
	if t.Contribution.V5Coordinate != h.spec.Coordinate || t.Owner != h.spec.Owner || t.Contribution.InstanceGeneration != h.spec.Generation {
		return plugin.ErrV5ContributionStale
	}
	out, err := h.call(ctx, "Invoke", "invoke", raw)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}
func (h *v5Running) Event(ctx context.Context, raw json.RawMessage) error {
	out, err := h.call(ctx, "Event", "event", raw)
	if err == nil && string(out) != `{"ack":true}` {
		return containerwire.ErrProtocol
	}
	return err
}
func (h *v5Running) Health(ctx context.Context) error {
	if h.busy.Load() {
		return nil
	}
	_, err := h.call(ctx, "Health", "health", nil)
	if err == errContainerBusy {
		return nil
	}
	return err
}
func (h *v5Running) call(ctx context.Context, method, kind string, raw json.RawMessage) (json.RawMessage, error) {
	if !h.busy.CompareAndSwap(false, true) {
		return nil, errContainerBusy
	}
	defer h.busy.Store(false)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	select {
	case <-h.done:
		return nil, plugin.ErrV5RuntimeMissing
	default:
	}
	if len(raw) > 65536 {
		return nil, containerwire.ErrProtocol
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(h.spec.Effective.TimeoutMS)*time.Millisecond)
	defer cancel()
	stop := context.AfterFunc(callCtx, h.Abort)
	defer stop()
	f := containerwire.Frame{Kind: kind, Payload: raw}
	for n := 0; n <= 32; n++ {
		reply, err := h.client.Exchange(callCtx, method, f)
		if err != nil || callCtx.Err() != nil {
			h.Abort()
			return nil, containerwire.ErrProtocol
		}
		if (kind == "configure" || kind == "health") && reply.Kind == "ready" && !reply.Failed && len(reply.Payload) == 0 && reply.Capability == "" {
			return nil, nil
		}
		if (kind == "invoke" || kind == "event") && reply.Kind == "result" && reply.Capability == "" && !reply.Failed && len(reply.Payload) <= 65536 && json.Valid(reply.Payload) {
			return reply.Payload, nil
		}
		if (kind == "invoke" || kind == "event") && reply.Kind == "result" && reply.Capability == "" && reply.Failed && len(reply.Payload) == 0 {
			return nil, plugin.ErrV5Denied
		}
		if reply.Kind != "host" || kind != "invoke" && kind != "event" || reply.Failed || n >= 32 || reply.Capability == "" || len(reply.Capability) > 128 || len(reply.Payload) > 65536 || !json.Valid(reply.Payload) {
			h.Abort()
			return nil, containerwire.ErrProtocol
		}
		declared := false
		for _, c := range h.spec.Container.Consumes() {
			declared = declared || c.Capability == reply.Capability
		}
		var result json.RawMessage
		err = plugin.ErrV5Denied
		if declared {
			token, _ := h.credential.Load().(string)
			result, err = h.broker(callCtx, token, reply.Capability, reply.Payload)
		}
		if len(result) > 65536 || len(result) > 0 && !json.Valid(result) {
			result = nil
			err = containerwire.ErrProtocol
		}
		f = containerwire.Frame{Kind: "host_result", Payload: result, Failed: err != nil}
		method = "HostResult"
	}
	h.Abort()
	return nil, containerwire.ErrProtocol
}

func (h *v5Running) checkControls(ctx context.Context) error {
	raw, err := h.factory.output(ctx, "inspect", "--format={{json .}}", h.name)
	if err != nil {
		return ErrImageUnavailable
	}
	var x struct {
		Image  string
		Mounts []struct{ Type, Destination string }
		State  struct{ Running bool }
		Config struct {
			User, WorkingDir string
			Env              []string
			Volumes          map[string]any
		}
		HostConfig struct {
			Privileged, ReadonlyRootfs, PublishAllPorts bool
			NetworkMode, IpcMode, PidMode, CgroupnsMode string
			CapAdd, CapDrop, SecurityOpt, Binds         []string
			Memory, MemorySwap, NanoCpus, PidsLimit     int64
			PortBindings                                map[string]any
			Devices                                     []any
			Tmpfs                                       map[string]string
			LogConfig                                   struct{ Type string }
		}
	}
	if json.Unmarshal(raw, &x) != nil {
		return ErrImageIdentity
	}
	r := h.spec.Effective
	c := x.HostConfig
	if x.Image != h.image || !x.State.Running || x.Config.User != "65534:65534" || x.Config.WorkingDir != "/tmp" || len(x.Config.Volumes) != 0 || len(x.Mounts) != 0 || c.Privileged || !c.ReadonlyRootfs || c.PublishAllPorts || c.NetworkMode != "none" || c.IpcMode != "none" || c.PidMode != "" || c.CgroupnsMode != "private" || len(c.CapAdd) != 0 || len(c.Binds) != 0 || len(c.PortBindings) != 0 || len(c.Devices) != 0 || c.LogConfig.Type != "none" || c.Memory != r.MemoryMB<<20 || c.MemorySwap != r.MemoryMB<<20 || c.NanoCpus != r.CPUMillis*1000000 || c.PidsLimit != 64 || len(c.CapDrop) != 1 || strings.ToUpper(c.CapDrop[0]) != "ALL" || len(c.SecurityOpt) != 1 || !strings.HasPrefix(c.SecurityOpt[0], "no-new-privileges") || len(c.Tmpfs) != 1 || c.Tmpfs["/tmp"] != "rw,noexec,nosuid,nodev,size="+strconv.FormatInt(r.StorageBytes, 10)+",mode=1777" {
		return ErrImageIdentity
	}
	if !slices.Contains(x.Config.Env, "CAMPUSOS_CONTAINER_TRANSPORT="+h.spec.Container.Transport()) {
		return ErrImageIdentity
	}
	return nil
}

type WorkerInfo struct {
	Name       string
	Coordinate plugin.V5Coordinate
	Owner      string
	Generation int64
}

func (f *V5Factory) Workers() []WorkerInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []WorkerInfo{}
	for _, h := range f.workers {
		out = append(out, WorkerInfo{h.name, h.spec.Coordinate, h.spec.Owner, h.spec.Generation})
	}
	return out
}
func (f *V5Factory) FenceOrphans(ctx context.Context) error {
	raw, err := f.output(ctx, "ps", "--all", "--filter=label=campusos.container.worker=v5", "--filter=label=campusos.container.namespace="+f.namespace, "--format={{.Names}}")
	if err != nil {
		return ErrImageUnavailable
	}
	for _, name := range strings.Fields(string(raw)) {
		if !ownedName.MatchString(name) {
			return ErrImageIdentity
		}
		if err := f.command(ctx, "rm", "--force", name).Run(); err != nil {
			return ErrImageUnavailable
		}
	}
	return nil
}
func (f *V5Factory) Close(ctx context.Context) error {
	f.mu.Lock()
	f.closed = true
	if f.cancel != nil {
		f.cancel()
	}
	workers := make([]*v5Running, 0, len(f.workers))
	for _, h := range f.workers {
		h.Abort()
		workers = append(workers, h)
	}
	f.mu.Unlock()
	waited := make(chan struct{})
	go func() { f.preparing.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-ctx.Done():
		return ctx.Err()
	}
	f.mu.Lock()
	workers = workers[:0]
	for _, h := range f.workers {
		h.Abort()
		workers = append(workers, h)
	}
	f.mu.Unlock()
	for _, h := range workers {
		if err := h.Close(ctx); err != nil {
			return err
		}
	}
	return os.RemoveAll(f.home)
}
