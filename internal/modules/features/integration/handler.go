package integration

import (
	"context"
	"net/http"
	"time"

	"github.com/campusos/CampusOS/internal/modules/features/ai"
	"github.com/campusos/CampusOS/internal/modules/features/mcp"
	"github.com/campusos/CampusOS/internal/modules/features/message"
	"github.com/campusos/CampusOS/internal/modules/features/personalspace"
	"github.com/campusos/CampusOS/internal/modules/features/webhook"
	pluginport "github.com/campusos/CampusOS/internal/plugin/port"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Card struct {
	Key       string                 `json:"key"`
	Title     string                 `json:"title"`
	Status    string                 `json:"status"`
	Summary   string                 `json:"summary"`
	Metrics   map[string]interface{} `json:"metrics,omitempty"`
	Links     []Link                 `json:"links,omitempty"`
	UpdatedAt time.Time              `json:"updated_at"`
}

type Link struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// SummaryPort is the only dependency used by the Integration Overview at
// runtime. Individual integration modules adapt their internal services to a
// card without exposing repositories or service implementations to transport.
type SummaryPort interface {
	SummaryCard(context.Context) Card
}

type SummaryPortFunc func(context.Context) Card

func (f SummaryPortFunc) SummaryCard(ctx context.Context) Card { return f(ctx) }

type Handler struct {
	pool       *pgxpool.Pool
	cfg        *config.Config
	plugins    pluginport.Catalog
	aiSvc      *ai.Service
	spaceSvc   *space.Service
	webhookSvc *webhook.Service
	mcpSvc     *mcp.Service
	messageSvc *message.Service
	metrics    *observability.Collector
	summaries  []SummaryPort
}

type Option func(*Handler)

func WithPool(pool *pgxpool.Pool) Option {
	return func(h *Handler) { h.pool = pool }
}

func WithConfig(cfg *config.Config) Option {
	return func(h *Handler) { h.cfg = cfg }
}

func WithPluginCatalog(catalog pluginport.Catalog) Option {
	return func(h *Handler) { h.plugins = catalog }
}

func WithAIService(service *ai.Service) Option {
	return func(h *Handler) { h.aiSvc = service }
}

func WithSpaceService(service *space.Service) Option {
	return func(h *Handler) { h.spaceSvc = service }
}

func WithWebhookService(service *webhook.Service) Option {
	return func(h *Handler) { h.webhookSvc = service }
}

func WithMCPService(service *mcp.Service) Option {
	return func(h *Handler) { h.mcpSvc = service }
}

func WithMessageService(service *message.Service) Option {
	return func(h *Handler) { h.messageSvc = service }
}

func WithMetrics(metrics *observability.Collector) Option {
	return func(h *Handler) { h.metrics = metrics }
}

func WithSummaryPorts(ports ...SummaryPort) Option {
	return func(h *Handler) { h.summaries = append(h.summaries, ports...) }
}

func NewHandler(options ...Option) *Handler {
	h := &Handler{}
	for _, option := range options {
		option(h)
	}
	return h
}

func (h *Handler) Overview(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	cards := make([]Card, 0, len(h.summaries))
	for _, provider := range h.summaries {
		if provider != nil {
			cards = append(cards, provider.SummaryCard(ctx))
		}
	}
	if len(cards) == 0 {
		// Compatibility fallback for callers that still construct the legacy
		// Handler directly during the A10 migration.
		cards = []Card{
			h.databaseCard(ctx),
			h.pluginCard(ctx),
			h.aiCard(),
			h.spaceCard(ctx),
			h.webhookCard(ctx),
			h.mcpCard(ctx),
			h.messageCard(ctx),
			h.metricsCard(),
		}
	}
	response.Success(c, gin.H{"items": cards, "total": len(cards)})
}

func (h *Handler) Metrics(c *gin.Context) {
	if h.metrics == nil {
		response.Success(c, observability.Snapshot{})
		return
	}
	response.Success(c, h.metrics.Snapshot())
}

func (h *Handler) databaseCard(ctx context.Context) Card {
	card := baseCard("database", "数据库", "ok", "PostgreSQL 连接正常")
	card.Links = []Link{{Label: "pgAdmin", Path: "http://localhost:5050"}}
	if h.pool == nil {
		card.Status = "memory"
		card.Summary = "当前运行在内存模式"
		return card
	}
	if err := h.pool.Ping(ctx); err != nil {
		card.Status = "error"
		card.Summary = err.Error()
		return card
	}
	card.Metrics = map[string]interface{}{
		"users":      h.countTable(ctx, "users"),
		"threads":    h.countTable(ctx, "threads"),
		"categories": h.countTable(ctx, "categories"),
		"plugins":    h.countTable(ctx, "plugins"),
	}
	return card
}

func (h *Handler) pluginCard(ctx context.Context) Card {
	card := baseCard("plugins", "插件仓库", "ok", "插件管理器已初始化")
	card.Links = []Link{{Label: "插件管理", Path: "/plugins"}}
	if h.plugins == nil {
		card.Status = "unknown"
		card.Summary = "插件管理器未初始化"
		return card
	}
	plugins, err := h.plugins.List(ctx)
	if err != nil {
		card.Status = "warning"
		card.Summary = err.Error()
		return card
	}
	running := 0
	errors := 0
	for _, item := range plugins {
		if item.Status == "running" {
			running++
		}
		if item.Status == "error" {
			errors++
		}
	}
	card.Metrics = map[string]interface{}{"total": len(plugins), "running": running, "errors": errors}
	if errors > 0 {
		card.Status = "warning"
		card.Summary = "存在异常插件，请查看插件日志"
	}
	return card
}

func (h *Handler) aiCard() Card {
	card := baseCard("ai", "AI Gateway", "disabled", "AI Gateway 未启用")
	if h.aiSvc == nil {
		return card
	}
	status := h.aiSvc.Status()
	card.Metrics = map[string]interface{}{"enabled": status.Enabled, "ready": status.Ready, "provider": status.Provider, "logs": status.Logs}
	if status.Enabled && status.Ready {
		card.Status = "ok"
		card.Summary = "AI Gateway 已就绪"
	} else if status.Enabled && !status.Ready {
		card.Status = "warning"
		card.Summary = status.Error
	}
	return card
}

func (h *Handler) spaceCard(ctx context.Context) Card {
	card := baseCard("spaces", "个人主页与风格包", "ok", "个人主页服务已启用")
	if h.spaceSvc == nil {
		card.Status = "unknown"
		card.Summary = "个人主页服务未初始化"
		return card
	}
	summary, err := h.spaceSvc.AdminSummary(ctx)
	if err != nil {
		card.Status = "warning"
		card.Summary = err.Error()
		return card
	}
	card.Metrics = map[string]interface{}{
		"total_spaces":        summary.TotalSpaces,
		"public_spaces":       summary.PublicSpaces,
		"disabled_spaces":     summary.DisabledSpaces,
		"styled_spaces":       summary.StyledSpaces,
		"sync_enabled_spaces": summary.SyncEnabledSpaces,
		"sync_error_spaces":   summary.SyncErrorSpaces,
	}
	card.Links = []Link{{Label: "用户管理", Path: "/users"}}
	if summary.SyncErrorSpaces > 0 || summary.DisabledSpaces > 0 {
		card.Status = "warning"
		card.Summary = "存在禁用主页或同步异常"
	}
	return card
}

func (h *Handler) webhookCard(ctx context.Context) Card {
	card := baseCard("webhook", "Webhook", "disabled", "未配置 Webhook endpoint")
	card.Links = []Link{{Label: "集成中心", Path: "/integrations"}}
	if h.webhookSvc == nil {
		return card
	}
	summary, err := h.webhookSvc.Summary(ctx)
	if err != nil {
		card.Status = "warning"
		card.Summary = err.Error()
		return card
	}
	card.Metrics = map[string]interface{}{
		"endpoint_total": summary.EndpointTotal,
		"enabled_total":  summary.EnabledTotal,
		"delivery_total": summary.DeliveryTotal,
		"success_total":  summary.SuccessTotal,
		"failed_total":   summary.FailedTotal,
	}
	if summary.EnabledTotal > 0 {
		card.Status = "ok"
		card.Summary = "Webhook endpoint 已启用"
	}
	if summary.LastFailureTotal > 0 {
		card.Status = "warning"
		card.Summary = "最近 24 小时存在 Webhook 投递失败"
	}
	return card
}

func (h *Handler) mcpCard(ctx context.Context) Card {
	card := baseCard("mcp", "MCP 只读工具", "disabled", "MCP 只读工具未启用")
	if h.mcpSvc == nil {
		return card
	}
	summary, err := h.mcpSvc.Summary(ctx)
	if err != nil {
		card.Status = "warning"
		card.Summary = err.Error()
		return card
	}
	card.Metrics = map[string]interface{}{"enabled": summary.Enabled, "tool_total": summary.ToolTotal, "audit_total": summary.AuditTotal}
	if summary.Enabled {
		card.Status = "ok"
		card.Summary = "MCP 只读工具已启用"
	}
	return card
}

func (h *Handler) messageCard(ctx context.Context) Card {
	card := baseCard("message", "Message 协议", "ok", "local adapter 已启用")
	if h.messageSvc == nil {
		card.Status = "unknown"
		card.Summary = "Message 服务未初始化"
		return card
	}
	summary, err := h.messageSvc.Summary(ctx)
	if err != nil {
		card.Status = "warning"
		card.Summary = err.Error()
		return card
	}
	card.Metrics = map[string]interface{}{"adapter_total": summary.AdapterTotal, "message_total": summary.MessageTotal, "binding_total": summary.BindingTotal}
	return card
}

func (h *Handler) metricsCard() Card {
	card := baseCard("metrics", "基础观测", "ok", "API metrics 已启用")
	if h.metrics == nil {
		card.Status = "unknown"
		card.Summary = "metrics collector 未初始化"
		return card
	}
	snapshot := h.metrics.Snapshot()
	card.Metrics = map[string]interface{}{
		"request_total":   snapshot.RequestTotal,
		"error_total":     snapshot.ErrorTotal,
		"last_latency_ms": snapshot.LastLatencyMS,
	}
	// The operations card deliberately projects only fixed aggregate keys.
	// Never copy arbitrary metric labels here: metrics must not become a route
	// for users, document IDs, paths, or other high-cardinality data into Admin.
	for _, metric := range snapshot.Metrics {
		switch metric.Name {
		case "campusos_storage_objects":
			if status := metric.Labels["status"]; boundedStorageObjectStatus(status) {
				card.Metrics["storage_objects_"+status] = int64(metric.Value)
			}
		case "campusos_storage_reservations":
			if status := metric.Labels["status"]; boundedReservationStatus(status) {
				card.Metrics["storage_reservations_"+status] = int64(metric.Value)
			}
		case "campusos_storage_reconcile_differences":
			if boundedReconcileKind(metric.Labels["kind"]) {
				current, _ := card.Metrics["storage_reconcile_differences"].(int64)
				card.Metrics["storage_reconcile_differences"] = current + int64(metric.Value)
			}
		case "campusos_document_preview_jobs":
			if boundedPreviewStatus(metric.Labels["status"]) && boundedPreviewFormat(metric.Labels["format"]) {
				current, _ := card.Metrics["document_preview_jobs"].(int64)
				card.Metrics["document_preview_jobs"] = current + int64(metric.Value)
			}
		}
	}
	return card
}

func boundedStorageObjectStatus(value string) bool {
	switch value {
	case "pending", "ready", "deleting", "deleted", "quarantined", "missing":
		return true
	default:
		return false
	}
}

func boundedReservationStatus(value string) bool {
	return value == "pending" || value == "committed" || value == "released"
}

func boundedPreviewStatus(value string) bool {
	switch value {
	case "pending", "processing", "ready", "failed", "unsupported":
		return true
	default:
		return false
	}
}

func boundedPreviewFormat(value string) bool {
	switch value {
	case "text", "markdown", "campusdoc", "pdf", "docx", "unknown":
		return true
	default:
		return false
	}
}

func boundedReconcileKind(value string) bool {
	switch value {
	case "pending_object_expired", "reservation_expired", "metadata_missing_physical", "physical_without_metadata", "payload_hash_or_size_mismatch", "unsafe_path", "invalid_owner_directory", "ledger_mismatch", "legacy_unclassified":
		return true
	default:
		return false
	}
}

func (h *Handler) countTable(ctx context.Context, table string) int64 {
	if h.pool == nil {
		return 0
	}
	var total int64
	if err := h.pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE deleted_at IS NULL").Scan(&total); err != nil {
		return 0
	}
	return total
}

func baseCard(key, title, status, summary string) Card {
	return Card{
		Key:       key,
		Title:     title,
		Status:    status,
		Summary:   summary,
		UpdatedAt: time.Now().UTC(),
	}
}

func NotImplemented(c *gin.Context) {
	response.Error(c, http.StatusNotImplemented, 73004, "not implemented")
}
