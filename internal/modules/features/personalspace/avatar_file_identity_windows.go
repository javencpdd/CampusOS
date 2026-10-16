//go:build windows

package space

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func avatarFileIdentity(path string, info os.FileInfo) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return "", ErrSpaceFileNotFound
	}
	var id windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &id); err != nil {
		return "", err
	}
	return fmt.Sprintf("windows:%d:%d:%d:%d:%d", id.VolumeSerialNumber, id.FileIndexHigh, id.FileIndexLow, id.CreationTime.HighDateTime, id.CreationTime.LowDateTime), nil
}
