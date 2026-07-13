package plugin

import "sort"

type PermissionDescriptor struct {
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Risk        string `json:"risk"`
	Description string `json:"description"`
}

var permissionCatalog = []PermissionDescriptor{
	{Resource: "audit", Action: "write", Risk: "high", Description: "Write security or governance audit records."},
	{Resource: "config", Action: "read", Risk: "low", Description: "Read the calling plugin's configuration."},
	{Resource: "config", Action: "write", Risk: "medium", Description: "Update the calling plugin's configuration."},
	{Resource: "event", Action: "publish", Risk: "medium", Description: "Publish an event through the CampusOS event bus."},
	{Resource: "homepage", Action: "read", Risk: "low", Description: "Read homepage presentation configuration."},
	{Resource: "homepage", Action: "write", Risk: "high", Description: "Change the system homepage presentation."},
	{Resource: "log", Action: "write", Risk: "low", Description: "Write namespaced plugin logs."},
	{Resource: "managed_data", Action: "delete", Risk: "high", Description: "Delete a declared host-managed system record through Host API v2."},
	{Resource: "managed_data", Action: "read", Risk: "medium", Description: "Read declared host-managed system records through Host API v2."},
	{Resource: "managed_data", Action: "write", Risk: "high", Description: "Create or update declared host-managed system records through Host API v2."},
	{Resource: "notification", Action: "send", Risk: "high", Description: "Send a user-facing notification."},
	{Resource: "permission", Action: "check", Risk: "medium", Description: "Evaluate a user's CampusOS permission."},
	{Resource: "post", Action: "delete", Risk: "high", Description: "Delete a reply within an authorized governance scope."},
	{Resource: "reply", Action: "read", Risk: "low", Description: "Read reply data exposed by Host API."},
	{Resource: "richtext_article", Action: "read", Risk: "low", Description: "Read rich-text article data."},
	{Resource: "richtext_article", Action: "write", Risk: "high", Description: "Create or change rich-text article data."},
	{Resource: "richtext_asset", Action: "read", Risk: "low", Description: "Read rich-text asset metadata."},
	{Resource: "richtext_asset", Action: "write", Risk: "high", Description: "Upload or change rich-text assets."},
	{Resource: "schedule", Action: "read", Risk: "medium", Description: "Read the authorized user's schedule."},
	{Resource: "schedule", Action: "write", Risk: "high", Description: "Change the authorized user's schedule."},
	{Resource: "space", Action: "read", Risk: "medium", Description: "Read a personal space allowed by its visibility policy."},
	{Resource: "space", Action: "write", Risk: "high", Description: "Change the authorized user's personal space."},
	{Resource: "space_file", Action: "read", Risk: "medium", Description: "Read files in the authorized personal-space namespace."},
	{Resource: "space_file", Action: "write", Risk: "high", Description: "Create or change files in the authorized personal-space namespace."},
	{Resource: "storage", Action: "delete", Risk: "medium", Description: "Delete a key in the calling plugin's isolated storage."},
	{Resource: "storage", Action: "read", Risk: "low", Description: "Read a key in the calling plugin's isolated storage."},
	{Resource: "storage", Action: "write", Risk: "medium", Description: "Write a key in the calling plugin's isolated storage."},
	{Resource: "style", Action: "read", Risk: "low", Description: "Read style-pack metadata allowed for the current target."},
	{Resource: "thread", Action: "lock", Risk: "high", Description: "Lock or unlock a thread within an authorized category."},
	{Resource: "thread", Action: "pin", Risk: "high", Description: "Pin or unpin a thread within an authorized category."},
	{Resource: "thread", Action: "read", Risk: "low", Description: "Read thread data exposed by Host API."},
	{Resource: "thread", Action: "write", Risk: "high", Description: "Create or change thread data for an authorized subject."},
	{Resource: "user", Action: "read", Risk: "medium", Description: "Read the public Host API user projection."},
	{Resource: "web_theme", Action: "configure", Risk: "high", Description: "Configure system-provided Web style packs."},
	{Resource: "web_theme", Action: "read", Risk: "low", Description: "Read available Web style packs and current selection."},
}

func PermissionCatalog() []PermissionDescriptor {
	result := append([]PermissionDescriptor(nil), permissionCatalog...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Resource == result[j].Resource {
			return result[i].Action < result[j].Action
		}
		return result[i].Resource < result[j].Resource
	})
	return result
}

func IsKnownPermission(resource, action string) bool {
	for _, permission := range permissionCatalog {
		if permission.Resource == resource && permission.Action == action {
			return true
		}
	}
	return resource == "*" && action == "*"
}
