package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	"github.com/campusos/CampusOS/internal/plugin"
)

// This adapter is the sole bridge from resource contexts to installed plugin
// declarations. Feature modules never receive a Manager or authorization store.
func validateResourceSurface(ctx context.Context, manager *plugin.Manager, authorization *plugin.AuthorizationService, v *richtext.PluginUIInvocation, actionID string, permission plugin.PermissionChecker) (string, error) {
	denied := &richtext.PDFViewerAuthorizationError{Reason: "surface_unavailable", Message: "插件界面未注册、已停用或不支持该展示方式，请刷新后重新打开。"}
	if manager == nil || authorization == nil || !authorization.Available() {
		return "", denied
	}
	installed, ok := manager.GetPlugin(v.PluginKey)
	if !ok || installed == nil || installed.Manifest == nil || !installed.DesiredEnabled || installed.Status != plugin.StatusRunning || installed.FrontendState != plugin.FrontendLoaded {
		return "", denied
	}
	found := false
	for _, surface := range installed.Manifest.UI.Surfaces {
		if surface.ID != v.SurfaceID {
			continue
		}
		for _, presentation := range surface.Presentations {
			if presentation == v.Presentation {
				found = true
			}
		}
	}
	if !found {
		return "", denied
	}
	if actionID != "" {
		found = false
		for _, action := range installed.Manifest.UI.Actions {
			if action.ID != actionID || action.Kind != "open-surface" || action.SurfaceID != v.SurfaceID || action.Presentation != v.Presentation {
				continue
			}
			if action.Permission != "" {
				parts := strings.SplitN(action.Permission, ":", 2)
				if len(parts) != 2 || permission == nil {
					return "", denied
				}
				allowed, err := permission(ctx, v.UserID, parts[0], parts[1])
				if err != nil || !allowed {
					return "", denied
				}
			}
			found = true
		}
		if !found {
			return "", denied
		}
	}
	version, err := authorization.ActiveVersion(ctx, v.PluginKey)
	if err != nil {
		return "", denied
	}
	// Existing context_digest pins the immutable authorization version; no new
	// schema/table is required. Blank pre-upgrade contexts fail closed on read.
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s", version.ID, version.PackageDigest, v.PluginKey, v.SurfaceID)))
	return fmt.Sprintf("%x", digest[:]), nil
}
