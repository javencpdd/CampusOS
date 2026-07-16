package stylepack

import (
	"fmt"
	"strings"
)

const (
	EffectRuntimeSandboxWorker = "sandbox-worker.v1"
	MaxEffectBytes             = 32 * 1024
)

func ValidateEffectScript(input string) ValidationResult {
	var result ValidationResult
	script := strings.TrimSpace(input)
	if script == "" {
		result.addError("effect script is empty")
		return result.finish()
	}
	if len(script) > MaxEffectBytes {
		result.addError(fmt.Sprintf("effect script must not exceed %d bytes", MaxEffectBytes))
		return result.finish()
	}

	lower := strings.ToLower(script)
	blocked := []string{
		"fetch(", "xmlhttprequest", "websocket", "eventsource", "sendbeacon",
		"importscripts", "indexeddb", "localstorage", "sessionstorage", "caches.",
		"document", "window", "parent", "opener", "location", "navigator", "cookie",
		"postmessage", "onmessage", "eval(", "new function", "webassembly",
		"sharedarraybuffer", "atomics.", "import(", "export ", "<script", "</script",
	}
	for _, marker := range blocked {
		if strings.Contains(lower, marker) {
			result.addError("effect script contains unavailable capability: " + marker)
		}
	}
	if !strings.Contains(script, "CampusEffect.register(") {
		result.addError("effect script must call CampusEffect.register(...)")
	}
	return result.finish()
}
