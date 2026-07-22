package handler

import (
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/apperror"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type AdminAdmissionHandler struct {
	service *service.AdminAdmissionService
}

func NewAdminAdmissionHandler(admission *service.AdminAdmissionService) *AdminAdmissionHandler {
	return &AdminAdmissionHandler{service: admission}
}

type adminAdmissionTransitionRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

// List exposes only credential-free management-plane admission information.
// GET /api/v1/admin/identity/admin-accounts
func (h *AdminAdmissionHandler) List(c *gin.Context) {
	if h == nil || h.service == nil {
		response.WriteError(c, unavailableError(apperror.IdentityAdminAdmissionUnavailable))
		return
	}
	page, pageSize := adminAdmissionPage(c)
	items, total, err := h.service.List(c.Request.Context(), repository.AdminAccountListFilter{
		Status: strings.TrimSpace(c.Query("status")), Page: page, PageSize: pageSize,
	})
	if err != nil {
		response.WriteError(c, unavailableIfInternal(adminAdmissionErrorTranslator.Translate(err)))
		return
	}
	response.List(c, items, &response.Pagination{
		Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages(total, pageSize),
	})
}

// Get returns one admission record. It is separate from the generic user
// profile route so the management-plane status cannot leak to user APIs.
// GET /api/v1/admin/identity/admin-accounts/:id
func (h *AdminAdmissionHandler) Get(c *gin.Context) {
	if h == nil || h.service == nil {
		response.WriteError(c, unavailableError(apperror.IdentityAdminAdmissionUnavailable))
		return
	}
	userID, ok := numericIdentityID(c.Param("id"))
	if !ok {
		response.ErrorDescriptor(c, apperror.IdentityAdminAdmissionInvalid, nil)
		return
	}
	item, err := h.service.Get(c.Request.Context(), userID)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(adminAdmissionErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, item)
}

// Suspend atomically pauses management-plane admission and invalidates every
// target session. The expected version prevents a stale admin page from
// overwriting a newer state.
// POST /api/v1/admin/identity/admin-accounts/:id/suspend
func (h *AdminAdmissionHandler) Suspend(c *gin.Context) {
	h.transition(c, true)
}

// Restore only reactivates records explicitly paused by admission management;
// a revoked record still requires a role assignment and its existing checks.
// POST /api/v1/admin/identity/admin-accounts/:id/restore
func (h *AdminAdmissionHandler) Restore(c *gin.Context) {
	h.transition(c, false)
}

// ListAudits returns only authorization evidence for the admission lifecycle.
// GET /api/v1/admin/identity/admin-accounts/audits
func (h *AdminAdmissionHandler) ListAudits(c *gin.Context) {
	if h == nil || h.service == nil {
		response.WriteError(c, unavailableError(apperror.IdentityAdminAdmissionUnavailable))
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.service.ListAudits(c.Request.Context(), limit)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(adminAdmissionErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *AdminAdmissionHandler) transition(c *gin.Context, suspend bool) {
	if h == nil || h.service == nil {
		response.WriteError(c, unavailableError(apperror.IdentityAdminAdmissionUnavailable))
		return
	}
	actorID, actorOK := currentRoleActorID(c)
	userID, userOK := numericIdentityID(c.Param("id"))
	if !actorOK {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if !userOK {
		response.ErrorDescriptor(c, apperror.IdentityAdminAdmissionInvalid, nil)
		return
	}
	var request adminAdmissionTransitionRequest
	if err := requestutil.BindJSONStrict(c, &request); err != nil {
		response.ErrorDescriptor(c, apperror.IdentityAdminAdmissionInvalid, nil)
		return
	}
	command := service.AdminAdmissionCommand{ExpectedVersion: request.ExpectedVersion, Reason: request.Reason}
	var (
		item *service.AdminAdmissionView
		err  error
	)
	if suspend {
		item, err = h.service.Suspend(c.Request.Context(), actorID, userID, command)
	} else {
		item, err = h.service.Restore(c.Request.Context(), actorID, userID, command)
	}
	if err != nil {
		response.WriteError(c, unavailableIfInternal(adminAdmissionErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, item)
}

func adminAdmissionPage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func totalPages(total int64, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}
