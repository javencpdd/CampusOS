package container

import "golang.org/x/sys/windows"

func currentDockerPlatform() (dockerPlatform, error) {
	return dockerPlatformFor("windows", func() (string, string, error) {
		// Query the OS location only; do not probe or create redirected folders.
		programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DONT_VERIFY)
		if err != nil {
			return "", "", err
		}
		systemRoot, err := windows.GetSystemWindowsDirectory()
		return programFiles, systemRoot, err
	})
}
