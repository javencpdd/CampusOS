package plugin

import "context"

// PluginStatus 插件状态
type PluginStatus string

const (
	StatusInstalled PluginStatus = "installed"
	StatusEnabled   PluginStatus = "enabled"
	StatusRunning   PluginStatus = "running"
	StatusStopped   PluginStatus = "stopped"
	StatusError     PluginStatus = "error"
)

// Plugin 插件实例
type Plugin struct {
	ID          string       `json:"id"`
	Manifest    *Manifest    `json:"manifest"`
	Status      PluginStatus `json:"status"`
	ErrorMsg    string       `json:"error_message,omitempty"`
	Directory   string       `json:"directory"`
	InstalledBy string       `json:"installed_by"`
}

// EventMessage 传递给插件的事件消息
type EventMessage struct {
	Type    string      `json:"type"`
	Source  string      `json:"source"`
	Subject string      `json:"subject"`
	Data    interface{} `json:"data"`
}

// PluginResponse 插件对事件的响应
type PluginResponse struct {
	Allowed bool   `json:"allowed"`
	Message string `json:"message,omitempty"`
}

// Runtime 插件运行时接口
type Runtime interface {
	// Start 启动插件进程
	Start(ctx context.Context, plugin *Plugin) error
	// Stop 停止插件进程
	Stop(ctx context.Context, pluginName string) error
	// SendEvent 向插件发送事件
	SendEvent(ctx context.Context, pluginName string, event *EventMessage) (*PluginResponse, error)
	// HealthCheck 检查插件健康状态
	HealthCheck(ctx context.Context, pluginName string) error
	// IsRunning 检查插件是否在运行
	IsRunning(pluginName string) bool
	// Type 返回运行时类型
	Type() string
}
