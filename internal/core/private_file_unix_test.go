//go:build !windows

package core

import "os"

func makeKeyPublic(path string) error { return os.Chmod(path, 0644) }
