package plugin_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func v5ContainerNativeEnvironment(t *testing.T, home string) (string, string, []string) {
	t.Helper()
	programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		t.Fatal("native Program Files API", err)
	}
	systemRoot, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		t.Fatal("native system Windows directory API", err)
	}
	return filepath.Join(programFiles, `Docker\Docker\resources\bin\docker.exe`), "npipe:////./pipe/docker_engine", []string{"SystemRoot=" + systemRoot, "USERPROFILE=" + home, "TEMP=" + home, "TMP=" + home, "DOCKER_CONFIG=" + home}
}

func v5ContainerPrepareServer(cmd *exec.Cmd) {
	// The runner must share a Windows console. API failure is a failed
	// acceptance, never a Kill fallback or an event sent to process group zero.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func v5ContainerStopServer(cmd *exec.Cmd) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}

func v5ContainerTerminationSignal() os.Signal { return os.Interrupt }
