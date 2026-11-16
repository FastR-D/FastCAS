//go:build windows

package core

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protectPrivateFile(path string) error { return protectPrivateACL(path, "") }

// Set inheritance before creating any secret bytes inside the new directory.
func protectPrivateDirectory(path string) error { return protectPrivateACL(path, "OICI") }

func protectPrivateACL(path, inheritance string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	sd, err = windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;%s;FA;;;SY)(A;%s;FA;;;%s)", inheritance, inheritance, owner.String()))
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func verifyPrivateFile(path string, _ os.FileInfo) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return fmt.Errorf("key files must have a private ACL")
	}
	for i := uint16(0); i < dacl.AceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(dacl, uint32(i), &ace); err != nil {
			return err
		}
		// Fail closed for unfamiliar ACE layouts rather than interpreting their SID offsets.
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("unsupported key-file ACL entry")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !windows.EqualSid(sid, owner) && !sid.IsWellKnown(windows.WinLocalSystemSid) {
			return fmt.Errorf("key files must be private to the owner and system")
		}
	}
	return nil
}

func replaceKeyManifest(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// Windows directory handles do not support File.Sync. Manifest replacement
// uses MOVEFILE_WRITE_THROUGH; file contents are flushed before replacement.
func syncKeyDirectory(_ *os.File) error { return nil }
