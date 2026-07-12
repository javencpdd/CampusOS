package agentcontract

import (
	"context"
	"errors"
	"strings"
)

type Permission string

const (
	ReadPublicContent Permission = "content.public.read"
	ReadOwnSchedule   Permission = "schedule.own.read"
	InvokeMCP         Permission = "mcp.invoke"
	DatabaseAccess    Permission = "host.database"
	JWTPrivateKey     Permission = "host.jwt.private"
	UserToken         Permission = "campusos.user.token"
	FullFilesystem    Permission = "host.filesystem.full"
)

var forbidden = map[Permission]bool{DatabaseAccess: true, JWTPrivateKey: true, UserToken: true, FullFilesystem: true}

type RunnerManifest struct {
	ID              string       `json:"id" yaml:"id"`
	Runtime         string       `json:"runtime" yaml:"runtime"`
	Permissions     []Permission `json:"permissions" yaml:"permissions"`
	FilesystemRoots []string     `json:"filesystem_roots,omitempty" yaml:"filesystem_roots,omitempty"`
}

func (m RunnerManifest) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("runner ID is required")
	}
	if m.Runtime != "grpc" && m.Runtime != "wasm" && m.Runtime != "remote-http" {
		return errors.New("runner must use an isolated external runtime")
	}
	for _, p := range m.Permissions {
		if forbidden[p] {
			return errors.New("runner requests forbidden host capability")
		}
	}
	for _, root := range m.FilesystemRoots {
		if root == "/" || strings.TrimSpace(root) == "" {
			return errors.New("runner filesystem scope is too broad")
		}
	}
	return nil
}

type TaskRequest struct {
	UserID     string
	Capability string
	Input      map[string]interface{}
}
type TaskResult struct {
	Output  map[string]interface{}
	AuditID string
}
type AgentCore interface {
	Authorize(context.Context, string, Permission) error
	RecordAudit(context.Context, string, map[string]interface{}) (string, error)
}
type Runner interface {
	Run(context.Context, TaskRequest) (TaskResult, error)
}
type KnowledgeProvider interface {
	Search(context.Context, string, int) ([]map[string]interface{}, error)
}
type MCPProvider interface {
	ListTools(context.Context) ([]string, error)
	Call(context.Context, string, map[string]interface{}) (map[string]interface{}, error)
}

type SecretClass string

const (
	CampusOSCredential   SecretClass = "campusos-credential"
	InfrastructureSecret SecretClass = "infrastructure-secret"
	ThirdPartySecret     SecretClass = "third-party-secret"
	NonSensitive         SecretClass = "non-sensitive"
	EnvironmentSpecific  SecretClass = "environment-specific"
)

type SecretDecision struct {
	Export                   bool
	Redact                   bool
	RequiresReauthentication bool
	Reason                   string
}

func ExportDecision(class SecretClass, encryptedEnvelope, userConfirmed bool) SecretDecision {
	switch class {
	case CampusOSCredential, InfrastructureSecret:
		return SecretDecision{Redact: true, RequiresReauthentication: true, Reason: "CampusOS and infrastructure credentials are never exported"}
	case ThirdPartySecret:
		if encryptedEnvelope && userConfirmed {
			return SecretDecision{Export: true, Reason: "explicit encrypted third-party export"}
		}
		return SecretDecision{Redact: true, Reason: "third-party secrets require confirmation and an encrypted envelope"}
	case NonSensitive:
		return SecretDecision{Export: true}
	case EnvironmentSpecific:
		return SecretDecision{Export: true, RequiresReauthentication: true, Reason: "target environment must reconfirm this value"}
	default:
		return SecretDecision{Redact: true, Reason: "unknown secret class is denied"}
	}
}
