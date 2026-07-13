package hostapi

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/campusos/CampusOS/internal/plugin"
)

var ErrHostAPIPermissionDenied = errors.New("host api permission denied")

type HostAPIPermission struct {
	Method   string `json:"method"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
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
	"RecordCreate":     {Resource: "managed_data", Action: "write"},
	"RecordGet":        {Resource: "managed_data", Action: "read"},
	"RecordList":       {Resource: "managed_data", Action: "read"},
	"RecordUpdate":     {Resource: "managed_data", Action: "write"},
	"RecordDelete":     {Resource: "managed_data", Action: "delete"},
}

func PermissionForMethod(method string) (HostAPIPermission, bool) {
	permission, ok := hostAPIMethodPermissions[method]
	permission.Method = method
	return permission, ok
}

func PermissionCatalog() []HostAPIPermission {
	result := make([]HostAPIPermission, 0, len(hostAPIMethodPermissions))
	for method, permission := range hostAPIMethodPermissions {
		permission.Method = method
		result = append(result, permission)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Method < result[j].Method })
	return result
}

func CheckHostAPIPermission(manifest *plugin.Manifest, method string) error {
	permission, ok := PermissionForMethod(method)
	if !ok {
		return nil
	}
	if manifest == nil {
		return fmt.Errorf("%w: plugin manifest is required for %s", ErrHostAPIPermissionDenied, method)
	}
	if strings.HasPrefix(method, "Record") && (!manifest.IsV2() || manifest.HostAPIVersion != plugin.HostAPIVersionV2) {
		return fmt.Errorf("%w: %s requires campusos.plugin/v2 and host_api_version %s", ErrHostAPIPermissionDenied, method, plugin.HostAPIVersionV2)
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
