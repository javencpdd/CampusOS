// This headless v5 package is a build/contract example. Activation and the
// runtime transport belong to Dispatcher/Runner stages, not package checking.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	campusos "github.com/campusos/CampusOS/sdk/go"
)

func main() {
	d := json.NewDecoder(io.LimitReader(os.Stdin, 65537))
	d.DisallowUnknownFields()
	var r campusos.V5ChatRequest
	if d.Decode(&r) != nil || r.RequestID == "" || r.ContributionID == "" || r.InstanceGeneration < 1 || r.DeadlineMS <= time.Now().UnixMilli() || len(r.Prompt) > 32768 || (r.ContextClass != "none" && r.ContextClass != "public") {
		fmt.Fprintln(os.Stderr, "provider.invalid_request")
		os.Exit(1)
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		fmt.Fprintln(os.Stderr, "provider.invalid_request")
		os.Exit(1)
	}
	result := campusos.V5ChatResponse{RequestID: r.RequestID, ContributionID: r.ContributionID, InstanceGeneration: r.InstanceGeneration, Text: "Echo: " + r.Prompt, FinishReason: "completed"}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		os.Exit(1)
	}
}
