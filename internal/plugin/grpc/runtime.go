package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
)

// GRPCRuntime gRPC 插件运行时
type GRPCRuntime struct {
	mu        sync.RWMutex
	processes map[string]*pluginProcess
}

type pluginProcess struct {
	cmd     *exec.Cmd
	plugin  *plugin.Plugin
	started time.Time
}

// NewGRPCRuntime 创建 gRPC 运行时
func NewGRPCRuntime() *GRPCRuntime {
	return &GRPCRuntime{
		processes: make(map[string]*pluginProcess),
	}
}

func (r *GRPCRuntime) Type() string { return "grpc" }

func (r *GRPCRuntime) Start(_ context.Context, p *plugin.Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.processes[p.ID]; ok {
		return fmt.Errorf("plugin '%s' already running", p.ID)
	}

	// 查找插件可执行文件
	binaryPath := p.Directory + "/plugin"
	cmd := exec.Command(binaryPath)
	cmd.Dir = p.Directory
	cmd.Stdout = &logWriter{pluginName: p.ID}
	cmd.Stderr = &logWriter{pluginName: p.ID}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start plugin process: %w", err)
	}

	r.processes[p.ID] = &pluginProcess{
		cmd:     cmd,
		plugin:  p,
		started: time.Now(),
	}

	// 监控进程退出
	go func() {
		err := cmd.Wait()
		r.mu.Lock()
		delete(r.processes, p.ID)
		r.mu.Unlock()
		if err != nil {
			log.Printf("⚠️  插件进程退出: %s (error: %v)", p.ID, err)
			p.Status = plugin.StatusError
			p.ErrorMsg = err.Error()
		}
	}()

	return nil
}

func (r *GRPCRuntime) Stop(_ context.Context, name string) error {
	r.mu.Lock()
	proc, ok := r.processes[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("plugin '%s' is not running", name)
	}
	r.mu.Unlock()

	// 发送 SIGTERM 优雅关闭
	if proc.cmd.Process != nil {
		proc.cmd.Process.Signal(syscall.SIGTERM)

		// 等待 5 秒后强制关闭
		done := make(chan error, 1)
		go func() {
			done <- proc.cmd.Wait()
		}()

		select {
		case <-done:
			// 正常退出
		case <-time.After(5 * time.Second):
			proc.cmd.Process.Kill()
		}
	}

	r.mu.Lock()
	delete(r.processes, name)
	r.mu.Unlock()

	return nil
}

func (r *GRPCRuntime) SendEvent(_ context.Context, pluginName string, event *plugin.EventMessage) (*plugin.PluginResponse, error) {
	r.mu.RLock()
	proc, ok := r.processes[pluginName]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("plugin '%s' is not running", pluginName)
	}

	// 尝试通过 HTTP 调用插件的 /event 端点
	pluginAddr := fmt.Sprintf("http://localhost:%d/event", 9000+proc.started.UnixNano()%1000)
	data, _ := json.Marshal(event)

	resp, err := http.Post(pluginAddr, "application/json", bytes.NewReader(data))
	if err != nil {
		// 插件不支持 HTTP，降级为日志记录
		log.Printf("📨 发送事件到插件 %s: %s (subject: %s) [HTTP不可用，仅记录]", pluginName, event.Type, event.Subject)
		return &plugin.PluginResponse{Allowed: true, Message: "processed (no HTTP)"}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result plugin.PluginResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return &plugin.PluginResponse{Allowed: true, Message: "processed"}, nil
	}
	return &result, nil
}

func (r *GRPCRuntime) HealthCheck(_ context.Context, pluginName string) error {
	r.mu.RLock()
	proc, ok := r.processes[pluginName]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin '%s' is not running", pluginName)
	}

	if proc.cmd.Process != nil {
		// 发送信号 0 检查进程是否存活
		err := proc.cmd.Process.Signal(syscall.Signal(0))
		if err != nil {
			return fmt.Errorf("plugin '%s' process not alive: %w", pluginName, err)
		}
	}
	return nil
}

func (r *GRPCRuntime) IsRunning(pluginName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.processes[pluginName]
	return ok
}

// logWriter 将插件输出重定向到日志
type logWriter struct {
	pluginName string
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	log.Printf("[plugin:%s] %s", w.pluginName, string(p))
	return len(p), nil
}

// StartHealthChecker 启动定期健康检查
func (r *GRPCRuntime) StartHealthChecker(ctx context.Context, interval time.Duration, manager *plugin.Manager) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.mu.RLock()
				names := make([]string, 0, len(r.processes))
				for name := range r.processes {
					names = append(names, name)
				}
				r.mu.RUnlock()

				for _, name := range names {
					if err := r.HealthCheck(ctx, name); err != nil {
						log.Printf("⚠️  插件健康检查失败: %s (%v)，尝试重启...", name, err)
						if p, ok := manager.GetPlugin(name); ok {
							r.Stop(ctx, name)
							if startErr := r.Start(ctx, p); startErr != nil {
								log.Printf("❌ 插件重启失败: %s (%v)", name, startErr)
							} else {
								log.Printf("✅ 插件已重启: %s", name)
							}
						}
					}
				}
			}
		}
	}()
}
