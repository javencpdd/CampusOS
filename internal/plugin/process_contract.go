package plugin

import (
	"errors"
	"fmt"
	"net/url"
)

const ProcessContractVersion = "campusos.process/v1"

func (m *Manifest) validateProcessContract() error {
	if m == nil || m.Runtime != "process" {
		return nil
	}
	if m.APIVersion != ManifestAPIVersionV3 {
		return errors.New("manifest: runtime process requires campusos.plugin/v3")
	}
	contract, _ := m.Config["process_contract"].(string)
	if contract != ProcessContractVersion {
		return fmt.Errorf("manifest: runtime process requires config.process_contract %q", ProcessContractVersion)
	}
	for _, key := range []string{"health_url", "extension_url"} {
		raw, _ := m.Config[key].(string)
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return fmt.Errorf("manifest: runtime process requires a valid loopback config.%s", key)
		}
		host := parsed.Hostname()
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			return fmt.Errorf("manifest: runtime process config.%s must use loopback", key)
		}
	}
	return nil
}
