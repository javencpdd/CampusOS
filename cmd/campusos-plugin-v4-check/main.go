// campusos-plugin-v4-check validates repository source packages. It does not
// install, execute, package, or otherwise trust any plugin.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/campusos/CampusOS/internal/plugin"
	pluginv4 "github.com/campusos/CampusOS/internal/plugin/v4"
)

func main() {
	root := flag.String("root", "plugins", "v4 plugin source root")
	flag.Parse()
	packages, err := pluginv4.FindSourcePackages(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, directory := range packages {
		manifest, err := pluginv4.ValidateSourceDirectory(directory)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", directory, err)
			os.Exit(1)
		}
		if err := manifest.ValidateCapabilities(func(code string) (pluginv4.CapabilityDescriptor, bool) {
			descriptor, ok := plugin.CapabilityByCode(code)
			return pluginv4.CapabilityDescriptor{Code: descriptor.Code, Scope: descriptor.Scope}, ok
		}); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", directory, err)
			os.Exit(1)
		}
		fmt.Printf("ok %s %s\n", manifest.Key, manifest.Version)
	}
}
