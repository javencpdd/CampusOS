package handler

import (
	"errors"
	"net/http"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type ChallengePolicyHandler struct {
	service *service.ChallengePolicyService
}

func NewChallengePolicyHandler(policyService *service.ChallengePolicyService) *ChallengePolicyHandler {
	return &ChallengePolicyHandler{service: policyService}
}

func (h *ChallengePolicyHandler) Get(c *gin.Context) {
	if h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "challenge policy is unavailable")
		return
	}
	policy, err := h.service.GetChallengePolicy(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "challenge policy is unavailable")
		return
	}
	response.Success(c, policy)
}

func (h *ChallengePolicyHandler) Update(c *gin.Context) {
	if h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "challenge policy is unavailable")
		return
	}
	actorID, ok := currentRoleActorID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var request domain.UpdateChallengePolicyRequest
	if err := requestutil.BindJSONStrict(c, &request); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid challenge policy")
		return
	}
	policy, err := h.service.UpdateChallengePolicy(c.Request.Context(), actorID, request)
	switch {
	case errors.Is(err, repository.ErrChallengePolicyVersionConflict):
		response.Error(c, http.StatusConflict, 10004, "challenge policy version conflict")
	case errors.Is(err, service.ErrChallengePolicyInvalid):
		response.Error(c, http.StatusBadRequest, 10001, "invalid challenge policy")
	case err != nil:
		response.Error(c, http.StatusServiceUnavailable, 10006, "challenge policy update failed")
	default:
		response.Success(c, policy)
	}
}
