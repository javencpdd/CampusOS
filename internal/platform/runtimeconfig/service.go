// Package runtimeconfig owns the deployment-controlled runtime file editor.
// Runtime application uses a closed catalog; confirmed full-file editing is
// restricted to the deployment-selected file, never a filesystem or shell API.
package runtimeconfig

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/config"
)

var ErrDenied = errors.New("configuration permission denied")
var ErrMFA = errors.New("current MFA required")
var ErrUnavailable = errors.New("configuration editor unavailable")
var ErrPending = errors.New("configuration restart is in progress")
var ErrVerificationInvalid = errors.New("configuration verification input is invalid")

type Field struct {
	config.RuntimeField
	Value       string `json:"value"`
	ActiveValue string `json:"active_value"`
	Configured  bool   `json:"configured"`
	Pending     bool   `json:"pending"`
}
type Snapshot struct {
	Available      bool     `json:"available"`
	Revision       string   `json:"revision"`
	Generation     string   `json:"generation"`
	Pending        bool     `json:"pending"`
	Restarting     bool     `json:"restarting"`
	Fields         []Field  `json:"fields"`
	DeploymentKeys []string `json:"deployment_keys"`
}
type Update struct {
	Revision           string            `json:"revision"`
	Changes            map[string]string `json:"changes"`
	Document           *string           `json:"document,omitempty"`
	Restart            bool              `json:"restart"`
	VerificationMethod string            `json:"verification_method,omitempty"`
	CurrentPassword    string            `json:"current_password,omitempty"`
}

// Password verification is supplied by Identity and fixed to this operation;
// ordinary Admin execution queries retain their existing MFA requirements.
func (s *Service) changeAuthority(ctx context.Context, resolve identityport.SessionPrincipalResolver, input Update, lock bool) (identityport.AdminExecutionAuthority, error) {
	var zero identityport.AdminExecutionAuthority
	if input.VerificationMethod != "password" {
		return s.authority(ctx, resolve, true, lock)
	}
	if s == nil || s.reliable == nil {
		return zero, ErrUnavailable
	}
	reader, ok := s.admins.(identityport.AdminRuntimeConfigPasswordReader)
	if !ok {
		return zero, ErrUnavailable
	}
	a, err := reader.LoadRuntimeConfigPasswordAuthority(ctx, resolve, input.CurrentPassword, lock)
	if err != nil {
		return zero, err
	}
	q := identityport.AdminExecutionQuery{PermissionCode: "platform.runtime_config.manage", Scope: identityport.PolicyResourceRef{Kind: "platform.runtime_config", ID: "current"}}
	if !a.Allowed || a.Query != q || identityport.CheckAdminExecutionAuthority(a, a) != nil || a.ExpiresAtMS <= time.Now().UnixMilli() {
		return zero, ErrDenied
	}
	return a, nil
}

type Service struct {
	mu         sync.Mutex
	cfg        config.ManagedConfig
	admins     identityport.AdminExecutionAuthorityReader
	reliable   *reliability.Service
	restart    func()
	generation string
	restarting bool
}

func New(cfg config.ManagedConfig, admins identityport.AdminExecutionAuthorityReader, reliable *reliability.Service, restart func()) *Service {
	var id [16]byte
	_, _ = rand.Read(id[:])
	return &Service{cfg: cfg, admins: admins, reliable: reliable, restart: restart, generation: hex.EncodeToString(id[:])}
}
func (s *Service) authority(ctx context.Context, resolve identityport.SessionPrincipalResolver, write, lock bool) (identityport.AdminExecutionAuthority, error) {
	var zero identityport.AdminExecutionAuthority
	if s == nil || s.admins == nil || s.reliable == nil {
		return zero, ErrUnavailable
	}
	code := "platform.runtime_config.read"
	if write {
		code = "platform.runtime_config.manage"
	}
	q := identityport.AdminExecutionQuery{PermissionCode: code, Scope: identityport.PolicyResourceRef{Kind: "platform.runtime_config", ID: "current"}}
	a, err := s.admins.LoadAdminExecutionAuthority(ctx, q, resolve, lock)
	if err != nil {
		return zero, err
	}
	if write && (!a.Principal.SatisfiesAdminMFARequirement() || a.ExpiresAtMS <= time.Now().UnixMilli()) {
		return zero, ErrMFA
	}
	if !a.Allowed || a.Query != q || identityport.CheckAdminExecutionAuthority(a, a) != nil || a.ExpiresAtMS <= time.Now().UnixMilli() {
		return zero, ErrDenied
	}
	return a, nil
}
func (s *Service) Read(ctx context.Context, resolve identityport.SessionPrincipalResolver) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a, err := s.authority(ctx, resolve, false, false)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Snapshot
	err = s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		current, e := s.authority(tx, resolve, false, true)
		if e != nil {
			return e
		}
		if e = identityport.CheckAdminExecutionAuthority(a, current); e != nil {
			return e
		}
		out, e = s.snapshot()
		return e
	})
	return out, err
}
func (s *Service) snapshot() (Snapshot, error) {
	out := Snapshot{Generation: s.generation, Restarting: s.restarting, Fields: []Field{}, DeploymentKeys: []string{}}
	if s.cfg.Path == "" || s.cfg.Error != nil {
		return out, nil
	}
	doc, err := config.ReadRuntimeDocument(s.cfg.Path)
	if err != nil {
		return out, ErrUnavailable
	}
	out.Available = true
	out.Revision = doc.Revision
	for _, f := range config.RuntimeFields() {
		active := s.cfg.Values[f.Key]
		value, ok := doc.Values[f.Key]
		if !ok {
			value = active
		}
		if f.Kind == "boolean" {
			value = normalizeBoolean(value)
			active = normalizeBoolean(active)
		}
		pending := value != active
		field := Field{RuntimeField: f, Value: value, ActiveValue: active, Configured: value != "", Pending: pending}
		if f.Kind == "secret" {
			field.Value = ""
			field.ActiveValue = ""
		}
		out.Fields = append(out.Fields, field)
		out.Pending = out.Pending || pending
	}
	return out, nil
}
func normalizeBoolean(v string) string {
	switch v {
	case "true", "1", "yes", "on":
		return "true"
	default:
		return "false"
	}
}

// Apply records an accepted intent before touching the filesystem. File rename
// cannot share a PostgreSQL transaction; a subsequent checked transaction holds
// current Admin authority while performing the rename. Audit failure therefore
// leaves the file untouched; a write failure leaves an honest intent record.
func (s *Service) Apply(ctx context.Context, resolve identityport.SessionPrincipalResolver, input Update) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if input.VerificationMethod != "" && input.VerificationMethod != "mfa" && input.VerificationMethod != "password" || input.VerificationMethod != "password" && input.CurrentPassword != "" || input.VerificationMethod == "password" && (len(input.CurrentPassword) == 0 || len(input.CurrentPassword) > 72) {
		return Snapshot{}, ErrVerificationInvalid
	}
	a, err := s.changeAuthority(ctx, resolve, input, false)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.restarting {
		return Snapshot{}, ErrPending
	}
	if s.cfg.Path == "" || s.cfg.Error != nil || s.restart == nil {
		return Snapshot{}, ErrUnavailable
	}
	doc, err := config.ReadRuntimeDocument(s.cfg.Path)
	if err != nil {
		return Snapshot{}, ErrUnavailable
	}
	if input.Revision != doc.Revision {
		return Snapshot{}, config.ErrManagedConflict
	}
	values := map[string]string{}
	for k, v := range s.cfg.Values {
		values[k] = v
	}
	for k, v := range doc.Values {
		values[k] = v
	}
	for _, f := range config.RuntimeFields() {
		if f.Kind == "boolean" {
			values[f.Key] = normalizeBoolean(values[f.Key])
		}
	}
	for k, v := range input.Changes {
		values[k] = v
	}
	raw, err := doc.Updated(input.Changes)
	if err != nil {
		return Snapshot{}, err
	}
	if input.Document == nil {
		if err = config.ValidateRuntimeValues(values); err != nil {
			return Snapshot{}, err
		}
	}
	keys := []string{}
	deploymentKeys := []string{}
	if input.Document != nil {
		if len(input.Changes) != 0 {
			return Snapshot{}, ErrVerificationInvalid
		}
		parsed, e := config.ParseRuntimeText([]byte(*input.Document))
		if e != nil {
			return Snapshot{}, e
		}
		original, e := config.ParseRuntimeText(doc.Raw)
		if e != nil {
			return Snapshot{}, e
		}
		known := map[string]bool{}
		for _, f := range config.RuntimeFields() {
			known[f.Key] = true
			values[f.Key] = s.cfg.Values[f.Key]
			if v, ok := parsed[f.Key]; ok {
				values[f.Key] = v
			}
			if f.Kind == "boolean" && (values[f.Key] == "1" || values[f.Key] == "yes" || values[f.Key] == "on") {
				values[f.Key] = "true"
			}
			if f.Kind == "boolean" && (values[f.Key] == "0" || values[f.Key] == "no" || values[f.Key] == "off") {
				values[f.Key] = "false"
			}
		}
		if e = config.ValidateRuntimeValues(values); e != nil {
			return Snapshot{}, e
		}
		all := map[string]bool{}
		for k := range parsed {
			all[k] = true
		}
		for k := range original {
			all[k] = true
		}
		for k := range all {
			old, oldOK := original[k]
			next, nextOK := parsed[k]
			if old != next || oldOK != nextOK {
				keys = append(keys, k)
				if !known[k] {
					deploymentKeys = append(deploymentKeys, k)
				}
			}
		}
		raw = []byte(*input.Document)
	}
	for k := range input.Changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sort.Strings(deploymentKeys)
	method := input.VerificationMethod
	if method == "" {
		method = "session-policy"
	}
	event, err := reliability.NewEvent("platform.runtime_config.intent", "runtime_config", "current", map[string]any{"keys": keys, "restart": input.Restart, "revision": doc.Revision, "verification_method": method, "document": input.Document != nil})
	if err != nil {
		return Snapshot{}, ErrUnavailable
	}
	check := func(tx context.Context) error {
		current, e := s.changeAuthority(tx, resolve, input, true)
		if e != nil {
			return e
		}
		return identityport.CheckAdminExecutionAuthority(a, current)
	}
	err = s.reliable.Execute(ctx, reliability.Command{Code: "platform.runtime_config.apply.intent", ActorType: "admin", ActorID: a.Principal.Actor.ID, ResourceType: "runtime_config", ResourceID: "current", OperationCode: "http.platform.runtime_config.update", PermissionCode: "platform.runtime_config.manage", Event: &event}, check)
	if err != nil {
		return Snapshot{}, ErrUnavailable
	}
	err = s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		if e := check(tx); e != nil {
			return e
		}
		if len(input.Changes) == 0 && input.Document == nil {
			current, err := config.ReadRuntimeDocument(s.cfg.Path)
			if err != nil {
				return ErrUnavailable
			}
			if current.Revision != input.Revision {
				return config.ErrManagedConflict
			}
			return nil
		}
		return config.ReplaceRuntimeDocument(s.cfg.Path, input.Revision, raw)
	})
	if err != nil {
		return Snapshot{}, err
	}
	out, err := s.snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	out.DeploymentKeys = deploymentKeys
	if input.Restart {
		s.restarting = true
		out.Restarting = true
	}
	return out, nil
}

// ScheduleRestart is called only after an acknowledged, audited apply. The
// grace delay lets the response reach the browser before HTTP drains.
func (s *Service) ScheduleRestart() {
	s.mu.Lock()
	ready := s.restarting
	restart := s.restart
	s.mu.Unlock()
	if ready && restart != nil {
		time.AfterFunc(750*time.Millisecond, restart)
	}
}

func publicError(err error) (int, string) {
	var syntax *config.RuntimeSyntaxError
	switch {
	case errors.As(err, &syntax):
		return 400, syntax.Error()
	case errors.Is(err, identityport.ErrAdminConfirmationPassword):
		return 400, "管理员密码不正确，请重新输入"
	case errors.Is(err, identityport.ErrAdminConfirmationThrottled):
		return 429, "密码验证尝试过于频繁，请稍后重试"
	case errors.Is(err, ErrVerificationInvalid):
		return 400, "请选择有效的验证方式并填写验证信息"
	case errors.Is(err, config.ErrManagedConflict), errors.Is(err, identityport.ErrPolicyFactsChanged):
		return 409, "配置已被其他操作修改，请重新加载后再保存"
	case errors.Is(err, ErrPending):
		return 409, "API 正在重启，请稍候"
	case errors.Is(err, ErrUnavailable), errors.Is(err, config.ErrManagedFile):
		return 503, "配置文件不可编辑，请检查部署的受管配置路径、文件权限或格式"
	case errors.Is(err, ErrDenied):
		return 403, "没有配置管理权限"
	default:
		// Validation messages contain catalog labels only, never input values.
		for _, f := range config.RuntimeFields() {
			if err.Error() == fmt.Sprintf("%s 格式或范围不正确", f.Label) {
				return 400, err.Error()
			}
		}
		for _, msg := range []string{"包含不支持的配置项或字符", "请填写有效的 SMTP 服务器", "请填写有效的发件人邮箱"} {
			if err.Error() == msg {
				return 400, msg
			}
		}
		return 503, "配置操作未确认，请重新加载检查保存结果"
	}
}
