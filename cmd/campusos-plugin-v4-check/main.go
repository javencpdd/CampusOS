// Retired protocol entry. v5 never adapts or installs a v4 package.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "plugin.legacy_protocol_removed: use campusos-plugin-v5-check or campusosctl plugin")
	os.Exit(1)
}
