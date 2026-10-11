package portadapt

import (
	"context"

	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
)

// AdminCodeChecker adapts AdminDomainService to the transport admin
// permission middleware.
type AdminCodeChecker struct {
	lifecycle *service.AdminDomainService
}

func NewAdminCodeChecker(lifecycle *service.AdminDomainService) *AdminCodeChecker {
	return &AdminCodeChecker{lifecycle: lifecycle}
}

func (c *AdminCodeChecker) HasAdminCode(ctx context.Context, accountID, code string) (bool, error) {
	return c.lifecycle.HasCode(ctx, accountID, code)
}
