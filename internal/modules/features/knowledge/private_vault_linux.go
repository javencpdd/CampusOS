package knowledge

import (
	"os"
	"syscall"
)

func privateVaultOwned(s os.FileInfo) bool {
	v, ok := s.Sys().(*syscall.Stat_t)
	return ok && v.Uid == uint32(os.Geteuid()) && (!s.Mode().IsRegular() || v.Nlink == 1)
}
