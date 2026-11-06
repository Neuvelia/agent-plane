//go:build windows

package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsStateDirectoryCreatedPrivateUnderBroadParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "broad-parent")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;GA;;;WD)(A;OICI;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(parent, "private-state")
	if err := verifyStateDir(state); err != nil {
		t.Fatalf("private state creation beneath broad parent: %v", err)
	}
	file, err := os.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := privateStateDirectory(file); err != nil {
		t.Fatalf("new directory inherited a broad ACL: %v", err)
	}
	if err := verifyStateDir(state); err != nil {
		t.Fatalf("reopening private state: %v", err)
	}
}

func TestWindowsStateDirectoryRejectsExistingBroadACL(t *testing.T) {
	state := filepath.Join(t.TempDir(), "broad-state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;WD)(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(state, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if err := verifyStateDir(state); !errors.Is(err, ErrDocumentSaveUnavailable) {
		t.Fatalf("existing broad state directory accepted: %v", err)
	}
}
