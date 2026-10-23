//go:build !windows

package plugin_test

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
)

func v5ContainerNativeEnvironment(t *testing.T, home string) (string, string, []string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Container acceptance requires a native Linux or Windows host")
	}
	return "/usr/bin/docker", "unix:///var/run/docker.sock", []string{"PATH=/usr/bin:/bin", "HOME=" + home, "TMPDIR=" + home, "DOCKER_CONFIG=" + home}
}

func v5ContainerPrepareServer(cmd *exec.Cmd) {}

func v5ContainerStopServer(cmd *exec.Cmd) error {
	return cmd.Process.Signal(syscall.SIGTERM)
}

func v5ContainerTerminationSignal() os.Signal { return syscall.SIGTERM }
