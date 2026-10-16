//go:build !windows

package space

import (
	"fmt"
	"os"
	"reflect"
)

func avatarFileIdentity(_ string, info os.FileInfo) (string, error) {
	if info == nil || !info.Mode().IsRegular() {
		return "", ErrSpaceFileNotFound
	}
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", ErrSpaceFactsUnavailable
	}
	dev, ino := v.FieldByName("Dev"), v.FieldByName("Ino")
	if !dev.IsValid() || !ino.IsValid() || !dev.CanInterface() || !ino.CanInterface() {
		return "", ErrSpaceFactsUnavailable
	}
	return fmt.Sprintf("unix:%v:%v", dev.Interface(), ino.Interface()), nil
}
