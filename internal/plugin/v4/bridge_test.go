package v4

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateBridgeRequest(t *testing.T) {
	valid := `{"version":"campusos.bridge/v1","request_id":"request-1","method":"resource.readRange","params":{"handle":"signed-resource-handle","offset":0,"length":1048576}}`
	if _, err := ValidateBridgeRequest([]byte(valid)); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	for name, request := range map[string]string{
		"unknown method":  strings.Replace(valid, "resource.readRange", "filesystem.read", 1),
		"oversized range": strings.Replace(valid, "1048576", "1048577", 1),
		"negative offset": strings.Replace(valid, `"offset":0`, `"offset":-1`, 1),
		"missing handle":  strings.Replace(valid, `"handle":"signed-resource-handle",`, "", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateBridgeRequest([]byte(request)); err == nil {
				t.Fatal("unsafe bridge request was accepted")
			}
		})
	}

	tooLarge := fmt.Sprintf(`{"version":"%s","request_id":"a","method":"config.read","params":{"padding":"%s"}}`, BridgeContractVersion, strings.Repeat("x", MaxBridgeMessageBytes))
	if _, err := ValidateBridgeRequest([]byte(tooLarge)); err == nil {
		t.Fatal("oversized message was accepted")
	}
}
