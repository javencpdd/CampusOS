package plugin

import (
	"fmt"
	"sort"
	"strings"
)

const CapabilityCatalogVersion = "campusos.capability/v1"

// CapabilityDescriptor is host-owned metadata. A manifest may request a
// capability, but it cannot lower its risk, consent, scope, or audit policy.
type CapabilityDescriptor struct {
	Code               string `json:"code" yaml:"code"`
	Resource           string `json:"resource" yaml:"resource"`
	Action             string `json:"action" yaml:"action"`
	Scope              string `json:"scope" yaml:"scope"`
	Risk               string `json:"risk" yaml:"risk"`
	ConsentRequired    bool   `json:"consent_required" yaml:"consent_required"`
	DataClassification string `json:"data_classification" yaml:"data_classification"`
	AuditLevel         string `json:"audit_level" yaml:"audit_level"`
	Description        string `json:"description" yaml:"description"`
}

var capabilityCatalog = []CapabilityDescriptor{
	{Code: "audit.system.write", Resource: "audit", Action: "write", Scope: "system", Risk: "high", DataClassification: "sensitive", AuditLevel: "decision", Description: "写入插件安全与治理审计记录"},
	{Code: "config.system.read", Resource: "config", Action: "read", Scope: "system", Risk: "low", DataClassification: "internal", AuditLevel: "summary", Description: "读取当前插件的非敏感配置"},
	{Code: "config.system.write", Resource: "config", Action: "write", Scope: "system", Risk: "medium", DataClassification: "internal", AuditLevel: "decision", Description: "修改当前插件的非敏感配置"},
	{Code: "event.system.publish", Resource: "event", Action: "publish", Scope: "system", Risk: "medium", DataClassification: "internal", AuditLevel: "decision", Description: "向受控事件总线发布事件"},
	{Code: "event.system.subscribe", Resource: "event", Action: "subscribe", Scope: "system", Risk: "medium", DataClassification: "internal", AuditLevel: "decision", Description: "订阅声明且获批的系统事件"},
	{Code: "homepage.system.read", Resource: "homepage", Action: "read", Scope: "system", Risk: "low", DataClassification: "public", AuditLevel: "summary", Description: "读取首页展示配置"},
	{Code: "homepage.system.write", Resource: "homepage", Action: "write", Scope: "system", Risk: "high", DataClassification: "internal", AuditLevel: "decision", Description: "修改系统首页展示配置"},
	{Code: "identity.self.permission_check", Resource: "permission", Action: "check", Scope: "self", Risk: "medium", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "检查当前授权用户自身的权限"},
	{Code: "managed_data.system.delete", Resource: "managed_data", Action: "delete", Scope: "system", Risk: "high", DataClassification: "internal", AuditLevel: "decision", Description: "删除插件声明的系统受管记录"},
	{Code: "managed_data.system.read", Resource: "managed_data", Action: "read", Scope: "system", Risk: "medium", DataClassification: "internal", AuditLevel: "decision", Description: "读取插件声明的系统受管记录"},
	{Code: "managed_data.system.write", Resource: "managed_data", Action: "write", Scope: "system", Risk: "high", DataClassification: "internal", AuditLevel: "decision", Description: "写入插件声明的系统受管记录"},
	{Code: "notification.self.send", Resource: "notification", Action: "send", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "向当前授权用户发送通知"},
	{Code: "observability.system.log", Resource: "log", Action: "write", Scope: "system", Risk: "low", DataClassification: "internal", AuditLevel: "summary", Description: "写入当前插件命名空间的脱敏日志"},
	{Code: "personal_space.self.read", Resource: "space", Action: "read", Scope: "self", Risk: "medium", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "读取当前授权用户的个人空间"},
	{Code: "personal_space.self.write", Resource: "space", Action: "write", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "修改当前授权用户的个人空间"},
	{Code: "personal_space_file.self.read", Resource: "space_file", Action: "read", Scope: "self", Risk: "medium", ConsentRequired: true, DataClassification: "restricted", AuditLevel: "decision", Description: "读取当前授权用户的个人空间文件"},
	{Code: "personal_space_file.self.write", Resource: "space_file", Action: "write", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "restricted", AuditLevel: "decision", Description: "写入当前授权用户的个人空间文件"},
	{Code: "plugin_file.self.delete", Resource: "plugin_file", Action: "delete", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "restricted", AuditLevel: "decision", Description: "删除当前用户的插件受管文件"},
	{Code: "plugin_file.self.read", Resource: "plugin_file", Action: "read", Scope: "self", Risk: "medium", ConsentRequired: true, DataClassification: "restricted", AuditLevel: "decision", Description: "读取当前用户的插件受管文件"},
	{Code: "plugin_file.self.write", Resource: "plugin_file", Action: "write", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "restricted", AuditLevel: "decision", Description: "写入当前用户的插件受管文件"},
	{Code: "plugin_record.self.delete", Resource: "managed_data", Action: "delete", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "删除当前用户的插件受管记录"},
	{Code: "plugin_record.self.read", Resource: "managed_data", Action: "read", Scope: "self", Risk: "medium", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "读取当前用户的插件受管记录"},
	{Code: "plugin_record.self.write", Resource: "managed_data", Action: "write", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "写入当前用户的插件受管记录"},
	{Code: "plugin_search.self.read", Resource: "plugin_search", Action: "read", Scope: "self", Risk: "medium", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "搜索当前用户的插件受管记录"},
	{Code: "reply.system.read", Resource: "reply", Action: "read", Scope: "system", Risk: "low", DataClassification: "public", AuditLevel: "summary", Description: "读取公开回复稳定投影"},
	{Code: "schedule.self.read", Resource: "schedule", Action: "read", Scope: "self", Risk: "medium", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "读取当前授权用户的课表"},
	{Code: "schedule.self.write", Resource: "schedule", Action: "write", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "修改当前授权用户的课表"},
	{Code: "secret.self.read", Resource: "secret", Action: "read", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "restricted", AuditLevel: "decision", Description: "在运行时使用当前用户授权的密钥引用"},
	{Code: "secret.system.read", Resource: "secret", Action: "read", Scope: "system", Risk: "high", DataClassification: "restricted", AuditLevel: "decision", Description: "在运行时使用管理员配置的系统密钥引用"},
	{Code: "storage.system.delete", Resource: "storage", Action: "delete", Scope: "system", Risk: "medium", DataClassification: "internal", AuditLevel: "decision", Description: "删除当前插件隔离存储中的键"},
	{Code: "storage.system.read", Resource: "storage", Action: "read", Scope: "system", Risk: "low", DataClassification: "internal", AuditLevel: "summary", Description: "读取当前插件隔离存储中的键"},
	{Code: "storage.system.write", Resource: "storage", Action: "write", Scope: "system", Risk: "medium", DataClassification: "internal", AuditLevel: "decision", Description: "写入当前插件隔离存储中的键"},
	{Code: "style.system.read", Resource: "style", Action: "read", Scope: "system", Risk: "low", DataClassification: "public", AuditLevel: "summary", Description: "读取允许公开的风格包元数据"},
	{Code: "thread.system.read", Resource: "thread", Action: "read", Scope: "system", Risk: "low", DataClassification: "public", AuditLevel: "summary", Description: "读取公开帖子稳定投影"},
	{Code: "user.contact.read", Resource: "user_contact", Action: "read", Scope: "self", Risk: "high", ConsentRequired: true, DataClassification: "restricted", AuditLevel: "decision", Description: "读取当前授权用户的联系信息"},
	{Code: "user.public_profile.read", Resource: "user", Action: "read", Scope: "self", Risk: "medium", ConsentRequired: true, DataClassification: "sensitive", AuditLevel: "decision", Description: "读取当前授权用户的公开资料稳定投影"},
	{Code: "web_theme.system.configure", Resource: "web_theme", Action: "configure", Scope: "system", Risk: "high", DataClassification: "internal", AuditLevel: "decision", Description: "配置系统 Web 风格包"},
	{Code: "web_theme.system.read", Resource: "web_theme", Action: "read", Scope: "system", Risk: "low", DataClassification: "public", AuditLevel: "summary", Description: "读取系统 Web 风格包"},
}

func CapabilityCatalog() []CapabilityDescriptor {
	result := append([]CapabilityDescriptor(nil), capabilityCatalog...)
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result
}

func CapabilityByCode(code string) (CapabilityDescriptor, bool) {
	for _, item := range capabilityCatalog {
		if item.Code == code {
			return item, true
		}
	}
	return CapabilityDescriptor{}, false
}

func CapabilityForPermission(resource, action string) (CapabilityDescriptor, bool) {
	for _, item := range capabilityCatalog {
		if item.Resource == resource && item.Action == action {
			return item, true
		}
	}
	return CapabilityDescriptor{}, false
}

func ValidateCapabilityCatalog() error {
	seen := map[string]bool{}
	permissionPairs := map[string]bool{}
	for _, item := range capabilityCatalog {
		if item.Code == "" || item.Resource == "" || item.Action == "" || item.Description == "" {
			return fmt.Errorf("capability catalog contains an incomplete descriptor: %+v", item)
		}
		if seen[item.Code] {
			return fmt.Errorf("capability code %q is duplicated", item.Code)
		}
		seen[item.Code] = true
		if item.Scope != "self" && item.Scope != "system" {
			return fmt.Errorf("capability %s has invalid scope %q", item.Code, item.Scope)
		}
		if strings.Contains(item.Resource, "*") || strings.Contains(item.Action, "*") {
			return fmt.Errorf("capability %s contains a wildcard", item.Code)
		}
		pair := item.Resource + "/" + item.Action
		// A pair may deliberately have self and system variants, but never two
		// descriptors for the same pair and scope.
		key := pair + "/" + item.Scope
		if permissionPairs[key] {
			return fmt.Errorf("capability mapping %s is duplicated", key)
		}
		permissionPairs[key] = true
	}
	return nil
}
