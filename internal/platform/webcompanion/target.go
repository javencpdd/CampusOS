package webcompanion

import (
	"context"
	"github.com/campusos/CampusOS/internal/platform/security"
	"time"
)

func ValidateTarget(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return security.ValidatePublicHTTPS(ctx, target)
}
