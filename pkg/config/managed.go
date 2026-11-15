package config

// Managed runtime configuration deliberately excludes database connections,
// listeners, encryption keys and Compose settings. Those remain deployment
// authority. Only this catalog may override a container's environment snapshot.
import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrManagedFile = errors.New("managed configuration file is unavailable or unsupported")
var ErrManagedConflict = errors.New("configuration changed; reload before saving")

type RuntimeField struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Group   string   `json:"group"`
	Kind    string   `json:"kind"`
	Help    string   `json:"help"`
	Options []string `json:"options,omitempty"`
}

func RuntimeFields() []RuntimeField {
	return []RuntimeField{
		{Key: "AUTH_ADMIN_MFA_REQUIRED", Label: "管理员操作要求 MFA", Group: "登录安全", Kind: "boolean", Help: "启用后，敏感管理操作需要绑定验证器并完成二次验证。"},
		{Key: "EMAIL_PROVIDER", Label: "邮件服务", Group: "邮件", Kind: "select", Options: []string{"fake", "smtp"}, Help: "fake 仅用于开发，不会实际发送邮件。"},
		{Key: "EMAIL_SMTP_HOST", Label: "SMTP 服务器", Group: "邮件", Kind: "text"},
		{Key: "EMAIL_SMTP_PORT", Label: "SMTP 端口", Group: "邮件", Kind: "number"},
		{Key: "EMAIL_SMTP_USERNAME", Label: "SMTP 用户名", Group: "邮件", Kind: "text"},
		{Key: "EMAIL_SMTP_PASSWORD", Label: "SMTP 密码或授权码", Group: "邮件", Kind: "secret", Help: "留空保留原值；勾选清除才会删除已保存的凭据。"},
		{Key: "EMAIL_SMTP_FROM", Label: "发件人邮箱", Group: "邮件", Kind: "text"},
		{Key: "EMAIL_SMTP_TIMEOUT", Label: "SMTP 超时", Group: "邮件", Kind: "text", Help: "例如 10s，范围 1–60 秒。"},
		{Key: "EMAIL_SMTP_STARTTLS", Label: "启用 STARTTLS", Group: "邮件", Kind: "boolean"},
		{Key: "CAMPUSOS_PLUGIN_UI_PARENTS", Label: "插件界面允许来源", Group: "插件", Kind: "text", Help: "用空格分隔完整来源，例如 http://localhost:3000 http://localhost:3001；不要填写路径或通配符。"},
		{Key: "AI_ENABLED", Label: "启用 AI", Group: "AI", Kind: "boolean", Help: "模型端点、密钥和绑定请在模型管理中设置。"},
		{Key: "AI_TIMEOUT", Label: "AI 请求超时", Group: "AI", Kind: "text", Help: "例如 30s，范围 1–60 秒。"},
		{Key: "AI_MAX_OUTPUT_TOKENS", Label: "AI 单次输出令牌上限", Group: "AI", Kind: "number"},
		{Key: "AI_MAX_CONCURRENT", Label: "AI 并发上限", Group: "AI", Kind: "number"},
	}
}

type ManagedConfig struct {
	Path   string
	Values map[string]string
	Error  error
}

type RuntimeDocument struct {
	Raw      []byte
	Revision string
	Values   map[string]string
}

// ReadRuntimeDocument accepts one-line literal dotenv assignments. Shell and
// Compose interpolation is deliberately not evaluated by the API. Unknown keys
// and comments are preserved verbatim. Only the separately confirmed full-file
// operation may disclose Raw; ordinary snapshots never return it.
func ReadRuntimeDocument(path string) (RuntimeDocument, error) {
	var zero RuntimeDocument
	info, err := safeRuntimeFile(path)
	if err != nil {
		return zero, err
	}
	f, err := os.Open(path)
	if err != nil {
		return zero, ErrManagedFile
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() > 256<<10 {
		return zero, ErrManagedFile
	}
	raw, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if err != nil || len(raw) > 256<<10 {
		return zero, ErrManagedFile
	}
	values := map[string]string{}
	known := map[string]bool{}
	for _, field := range RuntimeFields() {
		known[field.Key] = true
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
		key = strings.TrimSpace(key)
		if !ok || !known[key] {
			continue
		}
		if _, duplicate := values[key]; duplicate {
			return zero, ErrManagedFile
		}
		parsed, err := runtimeLiteral(strings.TrimSpace(value))
		if err != nil {
			return zero, err
		}
		values[key] = parsed
	}
	return RuntimeDocument{Raw: raw, Revision: fmt.Sprintf("%x", sha256.Sum256(raw)), Values: values}, nil
}

func safeRuntimeFile(path string) (os.FileInfo, error) {
	if path == "" {
		return nil, ErrManagedFile
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrManagedFile
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil || real != abs {
		return nil, ErrManagedFile
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return nil, ErrManagedFile
	}
	return info, nil
}

func runtimeLiteral(value string) (string, error) {
	if strings.HasPrefix(value, "'") {
		var out strings.Builder
		for i := 1; i < len(value); i++ {
			if value[i] == '\\' && i+1 < len(value) && value[i+1] == '\'' {
				out.WriteByte('\'')
				i++
				continue
			}
			if value[i] == '\'' {
				tail := strings.TrimSpace(value[i+1:])
				if tail != "" && !strings.HasPrefix(tail, "#") {
					return "", ErrManagedFile
				}
				return out.String(), nil
			}
			out.WriteByte(value[i])
		}
		return "", ErrManagedFile
	}
	if strings.HasPrefix(value, "\"") {
		for i := 1; i < len(value); i++ {
			if value[i] == '\\' {
				i++
				continue
			}
			if value[i] != '"' {
				continue
			}
			tail := strings.TrimSpace(value[i+1:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return "", ErrManagedFile
			}
			v, e := strconv.Unquote(value[:i+1])
			if e != nil {
				return "", ErrManagedFile
			}
			var out strings.Builder
			for j := 0; j < len(v); j++ {
				if v[j] == '$' {
					if j+1 >= len(v) || v[j+1] != '$' {
						return "", ErrManagedFile
					}
					j++
				}
				out.WriteByte(v[j])
			}
			return out.String(), nil
		}
		return "", ErrManagedFile
	}
	if strings.ContainsAny(value, "$`\\\"") {
		return "", ErrManagedFile
	}
	if before, _, ok := strings.Cut(value, " #"); ok {
		value = strings.TrimSpace(before)
	}
	return value, nil
}

func ValidateRuntimeValues(values map[string]string) error {
	for _, field := range RuntimeFields() {
		v := values[field.Key]
		bad := len(v) > 4096 || strings.ContainsAny(v, "\r\n\x00")
		switch field.Kind {
		case "boolean":
			bad = bad || v != "true" && v != "false"
		case "number":
			n, e := strconv.Atoi(v)
			max := 100000
			if field.Key == "EMAIL_SMTP_PORT" || field.Key == "AI_MAX_OUTPUT_TOKENS" {
				max = 65535
				if field.Key == "AI_MAX_OUTPUT_TOKENS" {
					max = 65536
				}
			}
			if field.Key == "AI_MAX_CONCURRENT" {
				max = 32
			}
			bad = bad || e != nil || n < 1 || n > max
		case "select":
			bad = bad || v != "fake" && v != "smtp"
		}
		if strings.HasSuffix(field.Key, "_TIMEOUT") {
			d, e := time.ParseDuration(v)
			bad = bad || e != nil || d < time.Second || d > time.Minute
		}
		if field.Key == "CAMPUSOS_PLUGIN_UI_PARENTS" {
			for _, origin := range strings.Fields(v) {
				u, e := url.Parse(origin)
				if e != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Scheme != "http" && u.Scheme != "https" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(origin, "*") {
					bad = true
				}
			}
		}
		if bad {
			return fmt.Errorf("%s 格式或范围不正确", field.Label)
		}
	}
	if values["EMAIL_PROVIDER"] == "smtp" {
		if strings.TrimSpace(values["EMAIL_SMTP_HOST"]) == "" || strings.ContainsAny(values["EMAIL_SMTP_HOST"], " /:@") {
			return errors.New("请填写有效的 SMTP 服务器")
		}
		if _, e := mail.ParseAddress(values["EMAIL_SMTP_FROM"]); e != nil {
			return errors.New("请填写有效的发件人邮箱")
		}
	}
	return nil
}

func (d RuntimeDocument) Updated(changes map[string]string) ([]byte, error) {
	known := map[string]bool{}
	for _, f := range RuntimeFields() {
		known[f.Key] = true
	}
	for key, v := range changes {
		if !known[key] || len(v) > 4096 || strings.ContainsAny(v, "\r\n\x00") {
			return nil, errors.New("包含不支持的配置项或字符")
		}
	}
	lines := strings.Split(string(d.Raw), "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		key, previous, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
		key = strings.TrimSpace(key)
		if v, changed := changes[key]; ok && changed {
			lines[i] = key + "=" + quoteRuntimeLiteral(v) + runtimeInlineComment(previous)
			seen[key] = true
		}
	}
	for _, f := range RuntimeFields() {
		if v, ok := changes[f.Key]; ok && !seen[f.Key] {
			lines = append(lines, f.Key+"="+quoteRuntimeLiteral(v))
		}
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"), nil
}

// ReplaceRuntimeDocument writes a private, fixed-name backup then atomically
// replaces the configured file. A stale editor never overwrites a newer file.
func ReplaceRuntimeDocument(path, revision string, raw []byte) error {
	current, err := ReadRuntimeDocument(path)
	if err != nil {
		return err
	}
	if current.Revision != revision {
		return ErrManagedConflict
	}
	backup := path + ".bak"
	if info, e := os.Lstat(backup); e == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return ErrManagedFile
	}
	if err := atomicRuntimeWrite(backup, current.Raw); err != nil {
		return err
	}
	check, err := ReadRuntimeDocument(path)
	if err != nil {
		return err
	}
	if check.Revision != revision {
		return ErrManagedConflict
	}
	return atomicRuntimeWrite(path, raw)
}

func atomicRuntimeWrite(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".env.campusos-config-*")
	if err != nil {
		return ErrManagedFile
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		return ErrManagedFile
	}
	return nil
}

// Compose single-quoted literals cannot end in a backslash. Double quotes
// handle that case, escaping dollars for Compose's interpolation pass.
func quoteRuntimeLiteral(v string) string {
	if strings.HasSuffix(v, "\\") {
		return strings.ReplaceAll(strconv.Quote(v), "$", "$$")
	}
	return "'" + strings.ReplaceAll(v, "'", "\\'") + "'"
}

func runtimeInlineComment(value string) string {
	var quote byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '#' && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t' || value[i-1] == '\'' || value[i-1] == '"') {
			return " " + value[i:]
		}
	}
	return ""
}
