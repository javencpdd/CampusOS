package hostapi

import (
	"errors"
	"fmt"

	"github.com/campusos/CampusOS/internal/plugin"
)

var ErrHostAPIPermissionDenied = errors.New("host api permission denied")

type HostAPIPermission struct {
	Resource string
	Action   string
}

var hostAPIMethodPermissions = map[string]HostAPIPermission{
	"GetUser":          {Resource: "user", Action: "read"},
	"GetThread":        {Resource: "thread", Action: "read"},
	"QueryThreads":     {Resource: "thread", Action: "read"},
	"GetReply":         {Resource: "reply", Action: "read"},
	"PublishEvent":     {Resource: "event", Action: "publish"},
	"SendNotification": {Resource: "notification", Action: "send"},
	"GetConfig":        {Resource: "config", Action: "read"},
	"SetConfig":        {Resource: "config", Action: "write"},
	"CheckPermission":  {Resource: "permission", Action: "check"},
	"Log":              {Resource: "log", Action: "write"},
	"StorageGet":       {Resource: "storage", Action: "read"},
	"StorageSet":       {Resource: "storage", Action: "write"},
	"StorageDelete":    {Resource: "storage", Action: "delete"},
}

func PermissionForMethod(method string) (HostAPIPermission, bool) {
	permission, ok := hostAPIMethodPermissions[method]
	return permission, ok
}

func CheckHostAPIPermission(manifest *plugin.Manifest, method string) error {
	permission, ok := PermissionForMethod(method)
	if !ok {
		return nil
	}
	if manifest == nil {
		return fmt.Errorf("%w: plugin manifest is required for %s", ErrHostAPIPermissionDenied, method)
	}
	if !manifest.HasPermission(permission.Resource, permission.Action) {
		return fmt.Errorf(
			"%w: plugin %s cannot call %s; requires %s/%s",
			ErrHostAPIPermissionDenied,
			manifest.Name,
			method,
			permission.Resource,
			permission.Action,
		)
	}
	return nil
}

func requireStorageOwner(manifest *plugin.Manifest, pluginName string) error {
	if manifest == nil {
		return fmt.Errorf("%w: plugin manifest is required for storage access", ErrHostAPIPermissionDenied)
	}
	if pluginName == "" || pluginName != manifest.Name {
		return fmt.Errorf(
			"%w: plugin %s cannot access storage namespace %q",
			ErrHostAPIPermissionDenied,
			manifest.Name,
			pluginName,
		)
	}
	return nil
}
