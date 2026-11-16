//go:build !windows

package core

import "os"

func protectPrivateFile(path string) error { return os.Chmod(path, 0600) }

func verifyPrivateFile(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return os.ErrPermission
	}
	return nil
}

func syncKeyDirectory(dir *os.File) error { return dir.Sync() }
