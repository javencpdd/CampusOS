package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// The owned Linux drill exports native Go results for an independent Ajv and
// state/realm oracle. This is outbound test evidence, not a runtime JSON codec.
func TestThreadOwnerReadPolicyNativeInterop(t *testing.T) {
	output := os.Getenv("CAMPUSOS_THREAD_OWNER_POLICY_OUTPUT")
	if output == "" {
		t.Skip("native owner policy output is supplied by the owner detail drill")
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	count := 0
	for _, kind := range []string{"anonymous", "user", "admin", "integration", "plugin_instance", "worker"} {
		for _, publication := range []string{"draft", "published", "private"} {
			for _, moderation := range []string{"clear", "pending", "rejected", "taken_down"} {
				for _, deletion := range []string{"active", "trashed", "purged"} {
					r := ownerReadRequest()
					r.RequestID = fmt.Sprintf("native-%d", count+1)
					r.Principal = ownerReadPrincipal(kind)
					r.Facts.PublicationStatus, r.Facts.ModerationStatus, r.Facts.DeletionStatus = publication, moderation, deletion
					d, err := (ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
					if err != nil {
						t.Fatal(err)
					}
					checkError := ""
					if err := port.CheckThreadOwnerReadDecision(r, d, r.Facts, r.Principal); err != nil {
						checkError = err.Error()
					}
					entry := struct {
						Name       string                       `json:"name"`
						Request    port.ThreadOwnerReadRequest  `json:"request"`
						Decision   port.ThreadOwnerReadDecision `json:"decision"`
						CheckError string                       `json:"check_error,omitempty"`
					}{Name: fmt.Sprintf("%s/%s/%s/%s", kind, publication, moderation, deletion), Request: r, Decision: d, CheckError: checkError}
					if err := encoder.Encode(entry); err != nil {
						t.Fatal(err)
					}
					count++
				}
			}
		}
	}
	if count != 216 {
		t.Fatalf("native matrix incomplete: %d", count)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
