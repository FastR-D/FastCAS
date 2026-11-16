//go:build windows

package core

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows does not translate Unix 0600 into an ACL. Remove inherited access
// and retain full access for the owner and the operating-system principal.
func protectPrivateFile(path string) error {
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;OW)")
	if err != nil {
		return fmt.Errorf("build private-file security descriptor: %w", err)
	}
	var present, defaulted int32
	var dacl *windows.ACL
	proc := syscall.NewLazyDLL("advapi32.dll").NewProc("GetSecurityDescriptorDacl")
	result, _, callErr := proc.Call(uintptr(unsafe.Pointer(sd)), uintptr(unsafe.Pointer(&present)), uintptr(unsafe.Pointer(&dacl)), uintptr(unsafe.Pointer(&defaulted)))
	if result == 0 {
		if callErr == syscall.Errno(0) {
			callErr = syscall.GetLastError()
		}
		return fmt.Errorf("read private-file ACL: %w", callErr)
	}
	if present == 0 || dacl == nil {
		return fmt.Errorf("private-file ACL is absent")
	}
	if err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("protect private file: %w", err)
	}
	return nil
}

func verifyPrivateFile(path string, _ os.FileInfo) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read private-file ACL: %w", err)
	}
	if !strings.Contains(sd.String(), "D:PAI(A;;FA;;;SY)(A;;FA;;;OW)") {
		return fmt.Errorf("key files must be private to the owner and system")
	}
	return nil
}

func syncKeyDirectory(_ *os.File) error { return nil }
