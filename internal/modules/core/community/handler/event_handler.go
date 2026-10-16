package handler

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	bus            *eventbus.MemoryEventBus
	adminSessions  identityport.AdminSessionAuthorityReader
	adminExecution identityport.AdminExecutionAuthorityReader
}

func NewEventHandler(bus *eventbus.MemoryEventBus) *EventHandler {
	return &EventHandler{bus: bus}
}

func (h *EventHandler) SetAdminAuthority(sessions identityport.AdminSessionAuthorityReader, execution identityport.AdminExecutionAuthorityReader) {
	h.adminSessions, h.adminExecution = sessions, execution
}

// ListEvents 获取事件历史
// GET /api/v1/events?limit=20
func (h *EventHandler) ListEvents(c *gin.Context) {
	if h.bus == nil {
		response.Error(c, http.StatusServiceUnavailable, 60001, "event history not available")
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	filter := c.Query("type")
	if err != nil || limit < 1 || limit > 100 || (filter != "" && eventbus.MetadataEventType(filter) != filter) {
		response.Error(c, 400, 10001, "事件类型或条数无效。")
		return
	}
	if h.adminExecution == nil {
		response.Error(c, 503, 60001, "暂时无法检查事件元数据权限。")
		return
	}
	ctx := c.Request.Context()
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		id := c.GetString("admin_account_id")
		if id == "" || id != c.GetString("user_id") {
			return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		return identityport.ResolveAdminSessionPrincipal(ctx, h.adminSessions, &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType})
	}
	query := identityport.AdminExecutionQuery{PermissionCode: "platform.reliability.read", Scope: identityport.PolicyResourceRef{Kind: "platform.event_metadata", ID: "recent"}}
	authority, err := h.adminExecution.LoadAdminExecutionAuthority(ctx, query, proof, false)
	if err != nil {
		writeEventMetadataError(c, err)
		return
	}
	if !authority.Allowed {
		response.Error(c, 403, 20004, "无权读取事件元数据。")
		return
	}
	initial, total := h.bus.GetMetadataHistory(limit, filter)
	current, err := h.adminExecution.LoadAdminExecutionAuthority(ctx, query, proof, false)
	if err != nil {
		writeEventMetadataError(c, err)
		return
	}
	if err := identityport.CheckAdminExecutionAuthority(authority, current); err != nil {
		writeEventMetadataError(c, err)
		return
	}
	events, currentTotal := h.bus.GetMetadataHistory(limit, filter)
	final, err := h.adminExecution.LoadAdminExecutionAuthority(ctx, query, proof, false)
	if err != nil {
		writeEventMetadataError(c, err)
		return
	}
	if err := identityport.CheckAdminExecutionAuthority(authority, final); err != nil {
		writeEventMetadataError(c, err)
		return
	}
	if total != currentTotal || !reflect.DeepEqual(initial, events) {
		writeEventMetadataError(c, identityport.ErrPolicyFactsChanged)
		return
	}
	if err := ctx.Err(); err != nil {
		writeEventMetadataError(c, err)
		return
	}
	response.Success(c, gin.H{
		"items": events,
		"total": currentTotal,
	})
}
func writeEventMetadataError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable):
		response.Error(c, 401, 20002, "请重新登录管理端。")
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.Error(c, 409, 10004, "事件或访问权限已变化，请重新加载。")
	default:
		response.Error(c, 503, 60001, "暂时无法检查事件元数据权限。")
	}
}
