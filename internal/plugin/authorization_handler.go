package plugin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) CapabilityCatalog(c *gin.Context) {
	response.Success(c, gin.H{"contract_version": CapabilityCatalogVersion, "items": CapabilityCatalog()})
}

func (h *Handler) AdminAuthorizationOverview(c *gin.Context) {
	service, ok := h.authorizationService(c)
	if !ok {
		return
	}
	overview, err := service.Overview(c.Request.Context(), c.Param("name"), "")
	if err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, overview)
}

func (h *Handler) MyAuthorizationOverview(c *gin.Context) {
	service, ok := h.authorizationService(c)
	if !ok {
		return
	}
	overview, err := service.Overview(c.Request.Context(), c.Param("name"), marketUserID(c))
	if err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, overview)
}

func (h *Handler) AdminSetCapabilityGrant(c *gin.Context) {
	service, ok := h.authorizationService(c)
	if !ok {
		return
	}
	versionID, err := strconv.ParseInt(c.Param("version_id"), 10, 64)
	if err != nil {
		h.authorizationError(c, errors.New("插件版本编号无效"))
		return
	}
	var input struct {
		Status    string                 `json:"status"`
		Reason    string                 `json:"reason"`
		Scope     map[string]interface{} `json:"scope"`
		ExpiresAt *time.Time             `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		h.authorizationError(c, err)
		return
	}
	grant, err := service.SetAdminGrant(c.Request.Context(), versionID, c.Param("capability"), input.Status, input.Reason, input.Scope, marketUserID(c), input.ExpiresAt)
	if err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, grant)
}

func (h *Handler) SetMyCapabilityConsent(c *gin.Context) {
	service, ok := h.authorizationService(c)
	if !ok {
		return
	}
	versionID, err := strconv.ParseInt(c.Param("version_id"), 10, 64)
	if err != nil {
		h.authorizationError(c, errors.New("插件版本编号无效"))
		return
	}
	var input struct {
		Status string                 `json:"status"`
		Scope  map[string]interface{} `json:"scope"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		h.authorizationError(c, err)
		return
	}
	consent, err := service.SetUserConsent(c.Request.Context(), marketUserID(c), versionID, c.Param("capability"), input.Status, input.Scope)
	if err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, consent)
}

func (h *Handler) IssueMyDelegation(c *gin.Context) {
	service, ok := h.authorizationService(c)
	if !ok {
		return
	}
	versionID, err := strconv.ParseInt(c.Param("version_id"), 10, 64)
	if err != nil {
		h.authorizationError(c, errors.New("插件版本编号无效"))
		return
	}
	var input struct {
		Capabilities []string               `json:"capabilities"`
		Scope        map[string]interface{} `json:"scope"`
		TTLSeconds   int64                  `json:"ttl_seconds"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		h.authorizationError(c, err)
		return
	}
	delegation, token, err := service.IssueDelegation(c.Request.Context(), marketUserID(c), versionID, input.Capabilities, input.Scope, time.Duration(input.TTLSeconds)*time.Second)
	if err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, gin.H{"delegation": delegation, "token": token, "warning": "委托令牌只显示一次，请仅交给对应插件运行时。"})
}

func (h *Handler) RevokeMyDelegation(c *gin.Context) {
	service, ok := h.authorizationService(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("delegation_id"), 10, 64)
	if err != nil {
		h.authorizationError(c, errors.New("委托编号无效"))
		return
	}
	if err := service.RevokeDelegation(c.Request.Context(), marketUserID(c), id); err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}

func (h *Handler) AdminAuthorizationDecisions(c *gin.Context) {
	service, ok := h.authorizationService(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := service.Decisions(c.Request.Context(), c.Param("name"), limit)
	if err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) SyncPluginAuthorization(c *gin.Context) {
	service, ok := h.authorizationService(c)
	if !ok {
		return
	}
	installed, found := h.manager.GetPlugin(c.Param("name"))
	if !found {
		h.authorizationError(c, ErrMarketNotFound)
		return
	}
	version, err := service.SyncInstalled(c.Request.Context(), installed, marketUserID(c))
	if err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, version)
}

func (h *Handler) ListSystemPluginSecrets(c *gin.Context) { h.listPluginSecrets(c, nil) }
func (h *Handler) ListMyPluginSecrets(c *gin.Context) {
	owner, ok := authenticatedNumericUser(c)
	if !ok {
		return
	}
	h.listPluginSecrets(c, &owner)
}
func (h *Handler) listPluginSecrets(c *gin.Context, owner *int64) {
	service, version, ok := h.secretContext(c)
	if !ok {
		return
	}
	items, err := service.List(c.Request.Context(), version.PluginID, owner)
	if err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}
func (h *Handler) SetSystemPluginSecret(c *gin.Context) { h.setPluginSecret(c, nil) }
func (h *Handler) SetMyPluginSecret(c *gin.Context) {
	owner, ok := authenticatedNumericUser(c)
	if !ok {
		return
	}
	h.setPluginSecret(c, &owner)
}
func (h *Handler) setPluginSecret(c *gin.Context, owner *int64) {
	service, version, ok := h.secretContext(c)
	if !ok {
		return
	}
	var input struct {
		Value string `json:"value"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		h.authorizationError(c, err)
		return
	}
	var actor *int64
	if value, valid := numericUserID(c); valid {
		actor = &value
	}
	metadata, err := service.Put(c.Request.Context(), version.PluginID, owner, c.Param("secret"), input.Value, actor)
	if err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, metadata)
}
func (h *Handler) RevokeSystemPluginSecret(c *gin.Context) { h.revokePluginSecret(c, nil) }
func (h *Handler) RevokeMyPluginSecret(c *gin.Context) {
	owner, ok := authenticatedNumericUser(c)
	if !ok {
		return
	}
	h.revokePluginSecret(c, &owner)
}
func (h *Handler) revokePluginSecret(c *gin.Context, owner *int64) {
	service, version, ok := h.secretContext(c)
	if !ok {
		return
	}
	if err := service.Revoke(c.Request.Context(), version.PluginID, owner, c.Param("secret")); err != nil {
		h.authorizationError(c, err)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}

func (h *Handler) secretContext(c *gin.Context) (*SecretService, PluginVersion, bool) {
	if h.secrets == nil || !h.secrets.Available() {
		response.Error(c, http.StatusServiceUnavailable, 60004, "插件 Secret Store 未配置；请设置 CAMPUSOS_PLUGIN_SECRET_KEY。")
		return nil, PluginVersion{}, false
	}
	if h.authorization == nil {
		response.Error(c, http.StatusServiceUnavailable, 60004, "插件授权服务暂不可用。")
		return nil, PluginVersion{}, false
	}
	version, err := h.authorization.store.ActiveVersion(c.Request.Context(), c.Param("name"))
	if err != nil {
		h.authorizationError(c, err)
		return nil, PluginVersion{}, false
	}
	return h.secrets, version, true
}
func numericUserID(c *gin.Context) (int64, bool) {
	value, err := strconv.ParseInt(marketUserID(c), 10, 64)
	return value, err == nil && value > 0
}
func authenticatedNumericUser(c *gin.Context) (int64, bool) {
	value, ok := numericUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "需要有效的登录用户。")
		return 0, false
	}
	return value, true
}

func (h *Handler) authorizationService(c *gin.Context) (*AuthorizationService, bool) {
	if h == nil || h.authorization == nil || !h.authorization.Available() {
		response.Error(c, http.StatusServiceUnavailable, 60004, "插件授权服务暂不可用，请确认数据库迁移已完成。")
		return nil, false
	}
	return h.authorization, true
}

func (h *Handler) authorizationError(c *gin.Context, err error) {
	status, code := http.StatusBadRequest, 60005
	if errors.Is(err, ErrMarketNotFound) {
		status, code = http.StatusNotFound, 60003
	}
	if errors.Is(err, ErrAuthorizationDenied) {
		status, code = http.StatusForbidden, 20004
	}
	message := strings.TrimSpace(err.Error())
	switch {
	case strings.Contains(message, "immutable"):
		message = "该插件版本已冻结，但当前包摘要或能力声明已发生变化；请提升插件版本后重新安装。"
	case strings.Contains(message, "unknown capability"):
		message = "能力代码不在宿主目录中，请更新插件声明后重试。"
	case strings.Contains(message, "is not declared by this version"):
		message = "当前插件版本没有声明该能力，不能进行授权。"
	case strings.Contains(message, "scope cannot exceed"):
		message = "所选资源范围超过该能力允许的最大范围，请缩小范围后重试。"
	case strings.Contains(message, "admin grant status"):
		message = "管理员授权状态无效，只能授予、拒绝或撤销。"
	case strings.Contains(message, "admin grant reason"):
		message = "请填写管理员授权操作原因（最多 500 字）。"
	case strings.Contains(message, "user consent status"):
		message = "用户授权状态无效，只能同意、拒绝或撤销。"
	case strings.Contains(message, "does not accept user consent"):
		message = "该能力不需要用户授权，只有管理员可以调整。"
	case strings.Contains(message, "valid authenticated user"):
		message = "登录状态无效，请重新登录后重试。"
	case strings.Contains(message, "delegation ttl"):
		message = "委托有效期必须大于 0 秒且不能超过 24 小时。"
	case strings.Contains(message, "delegation requires"):
		message = "请至少选择一项已获授权的能力后再生成委托。"
	case strings.Contains(message, "secret name is invalid"):
		message = "Secret 名称需以字母开头，只能包含字母、数字、点、下划线和连字符，最长 128 个字符。"
	case strings.Contains(message, "secret value must contain"):
		message = "Secret 值不能为空，且 UTF-8 编码后不能超过 65536 字节。"
	case strings.Contains(message, "secret service is unavailable"):
		message = "插件 Secret Store 暂不可用，请联系管理员检查加密密钥配置。"
	case strings.Contains(message, "version mismatch"):
		message = "插件版本已变化，请刷新页面后重新操作。"
	case message == "":
		message = "插件授权操作失败，请检查版本、能力和当前状态。"
	}
	response.Error(c, status, code, message)
}
