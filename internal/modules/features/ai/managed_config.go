package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/campusos/CampusOS/pkg/config"
)

type managedDeployment struct {
	Profile ManagedProfile
	Key     string
	Quota   int64
}

func managedConfiguration(cfg config.AIConfig) ([]managedDeployment, *security.Keyring, error) {
	keys := map[string][]byte{}
	for _, part := range strings.Split(cfg.EncryptionKeys, ",") {
		pieces := strings.SplitN(part, ":", 2)
		if len(pieces) != 2 || keys[pieces[0]] != nil {
			return nil, nil, ErrManagedUnavailable
		}
		b, e := hex.DecodeString(pieces[1])
		if e != nil {
			return nil, nil, ErrManagedUnavailable
		}
		keys[pieces[0]] = b
	}
	ring, e := security.NewKeyring(cfg.ActiveKeyID, keys)
	for _, b := range keys {
		clear(b)
	}
	if e != nil {
		return nil, nil, ErrManagedUnavailable
	}
	timeout, e := time.ParseDuration(cfg.Timeout)
	if e != nil || timeout < 100*time.Millisecond || timeout > 60*time.Second || cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 32 || cfg.MaxOutputTokens < 1 || cfg.MaxOutputTokens > 65536 || cfg.BudgetRequests < 1 || cfg.BudgetRequests > 1000000000 {
		return nil, nil, ErrManagedUnavailable
	}
	if cfg.DefaultProfile != "remote-chat" && cfg.DefaultProfile != "local-chat" {
		return nil, nil, ErrManagedUnavailable
	}
	configs := []managedDeployment{{Profile: ManagedProfile{ID: "remote-chat", Definition: "builtin.openai-compatible.text", Model: cfg.Model, Endpoint: strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"}, Key: cfg.APIKey}, {Profile: ManagedProfile{ID: "local-chat", Definition: "builtin.local-openai.text", Model: cfg.LocalModel, Endpoint: strings.TrimRight(cfg.LocalBaseURL, "/") + "/chat/completions"}, Key: cfg.LocalAPIKey}}
	for i := range configs {
		d := &configs[i]
		if !validText(d.Profile.Model, 256, true) || len(d.Key) > 4096 || strings.ContainsAny(d.Key, "\r\n\x00") || d.Profile.ID == "remote-chat" && d.Key == "" {
			return nil, nil, ErrManagedUnavailable
		}
		d.Quota = int64(cfg.BudgetRequests)
		d.Profile.Enabled = true
		d.Profile.TimeoutMS = timeout.Milliseconds()
		d.Profile.MaxConcurrent = cfg.MaxConcurrent
		d.Profile.MaxTokens = cfg.MaxOutputTokens
		u, e := url.Parse(d.Profile.Endpoint)
		if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" || len(u.String()) > 2048 || u.String() != d.Profile.Endpoint {
			return nil, nil, ErrManagedUnavailable
		}
		if d.Profile.ID == "local-chat" {
			if u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() || u.Port() == "" {
				return nil, nil, ErrManagedUnavailable
			}
		} else {
			if _, e = security.NewEgressBroker(security.EgressPolicy{AllowedOrigins: []string{u.Scheme + "://" + u.Host}, Timeout: timeout}); e != nil {
				return nil, nil, ErrManagedUnavailable
			}
		}
		digest := sha256.Sum256([]byte(d.Key))
		raw, _ := json.Marshal([]any{d.Profile.ID, d.Profile.Definition, d.Profile.Model, d.Profile.Endpoint, d.Profile.TimeoutMS, d.Profile.MaxTokens, d.Profile.MaxConcurrent, d.Quota, hex.EncodeToString(digest[:]), ring.ActiveKeyID()})
		sum := sha256.Sum256(raw)
		d.Profile.Fingerprint = hex.EncodeToString(sum[:])
	}
	return configs, ring, nil
}
func managedSecretAAD(p ManagedProfile, ref string) []byte {
	raw, _ := json.Marshal([]string{"campusos.ai-secret/v1", p.ID, p.Definition, p.Endpoint, "chat", ref})
	return raw
}
