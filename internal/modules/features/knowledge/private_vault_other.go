//go:build !linux

package knowledge

import "os"

// Native Windows protection remains unavailable until its ACL/erase contract is accepted.
func privateVaultOwned(os.FileInfo) bool { return false }
