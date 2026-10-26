//go:build !windows

package container

import "runtime"

func currentDockerPlatform() (dockerPlatform, error) {
	return dockerPlatformFor(runtime.GOOS, nil)
}
