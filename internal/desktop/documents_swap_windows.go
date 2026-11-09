//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows saves stay gated until native tests establish the directory
// durability and path-race contract. This helper preserves displaced bytes
// during a ReplaceFileW exchange for eventual recovery.
const documentSwapAvailable = false

var replaceFileProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func windowsCandidateName(from, to string) (string, error) {
	valid := func(name string) bool {
		return name != "." && name != ".." && name == filepath.Base(name) &&
			!strings.ContainsAny(name, "\\/:\x00")
	}
	if !valid(from) || !valid(to) || from == to {
		return "", ErrInvalidDocumentPath
	}
	fromCandidate := strings.HasPrefix(from, ".agentplane-save-") && strings.HasSuffix(from, ".candidate")
	toCandidate := strings.HasPrefix(to, ".agentplane-save-") && strings.HasSuffix(to, ".candidate")
	if fromCandidate == toCandidate {
		return "", ErrInvalidDocumentPath
	}
	if fromCandidate {
		return from, nil
	}
	return to, nil
}

func windowsPinnedDirectoryPath(parent *os.Root) (string, error) {
	dir, err := parent.Open(".")
	if err != nil {
		return "", err
	}
	defer dir.Close()
	var handleInfo windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(dir.Fd()), &handleInfo); err != nil ||
		handleInfo.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 ||
		handleInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return "", ErrInvalidDocumentPath
	}
	buffer := make([]uint16, 32768)
	length, err := windows.GetFinalPathNameByHandle(windows.Handle(dir.Fd()), &buffer[0], uint32(len(buffer)), 0)
	if err != nil || length == 0 || int(length) >= len(buffer) {
		return "", ErrInvalidDocumentPath
	}
	name := windows.UTF16ToString(buffer[:length])
	opened, err := dir.Stat()
	if err != nil {
		return "", ErrInvalidDocumentPath
	}
	current, err := os.Lstat(name)
	if err != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, current) {
		return "", ErrInvalidDocumentPath
	}
	return name, nil
}

func replaceWindowsFile(target, replacement, backup string) error {
	targetName, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	replacementName, err := windows.UTF16PtrFromString(replacement)
	if err != nil {
		return err
	}
	backupName, err := windows.UTF16PtrFromString(backup)
	if err != nil {
		return err
	}
	result, _, callErr := replaceFileProc.Call(uintptr(unsafe.Pointer(targetName)), uintptr(unsafe.Pointer(replacementName)), uintptr(unsafe.Pointer(backupName)), 0, 0, 0)
	if result != 0 {
		return nil
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
		return errno
	}
	return windows.ERROR_GEN_FAILURE
}

// ReplaceFileW provides an old-file backup but not a two-way name exchange.
// A second, no-clobber hard link restores the displaced name. A durable intent
// remains until the caller has verified both identities and removed it.
func swapDocumentEntries(parent *os.Root, from, to string) error {
	candidateName, err := windowsCandidateName(from, to)
	if err != nil {
		return err
	}
	backupName := candidateName + ".displaced"
	if _, err := parent.Lstat(backupName); err == nil || !errors.Is(err, os.ErrNotExist) {
		return ErrDocumentSaveInDoubt
	}
	base, err := windowsPinnedDirectoryPath(parent)
	if err != nil {
		return err
	}
	if err := replaceWindowsFile(filepath.Join(base, to), filepath.Join(base, from), filepath.Join(base, backupName)); err != nil {
		if code, ok := err.(syscall.Errno); ok {
			return fmt.Errorf("ReplaceFileW state %d: %w", classifyWindowsReplace(uint32(code)), ErrDocumentSaveInDoubt)
		}
		return fmt.Errorf("ReplaceFileW unavailable: %w", ErrDocumentSaveInDoubt)
	}
	backup, err := openRegularDocument(parent, backupName)
	if err != nil {
		return ErrDocumentSaveInDoubt
	}
	defer backup.Close()
	if err := linkDocumentExclusive(parent, backup, backupName, from); err != nil {
		return ErrDocumentSaveInDoubt
	}
	if err := parent.Remove(backupName); err != nil {
		return ErrDocumentSaveInDoubt
	}
	return nil
}

// reconcileWindowsSwap never guesses about an unexpected target. The journal
// carries the original identity and digest; a leftover backup is linked only
// to a missing name known to need that original.
func reconcileWindowsSwap(parent *os.Root, targetName string, intent saveIntent) error {
	backupName := intent.Candidate + ".displaced"
	backup, err := openRegularDocument(parent, backupName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		if _, statErr := parent.Lstat(backupName); errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		return ErrDocumentSaveInDoubt
	}
	defer backup.Close()
	identity, err := identityOfFile(backup)
	if err != nil || identity != intent.OriginalIdentity {
		return ErrDocumentSaveInDoubt
	}
	data, err := readBoundedDocument(backup)
	if err != nil || digestBytes(data) != intent.ExpectedDigest {
		return ErrDocumentSaveInDoubt
	}
	if _, err := parent.Lstat(targetName); errors.Is(err, os.ErrNotExist) {
		if err := linkDocumentExclusive(parent, backup, backupName, targetName); err != nil {
			return ErrDocumentSaveInDoubt
		}
	} else if err != nil {
		return ErrDocumentSaveInDoubt
	} else if _, err := parent.Lstat(intent.Candidate); errors.Is(err, os.ErrNotExist) {
		if !matchesWindowsSwapFile(parent, targetName, intent.CandidateIdentity, intent.CandidateDigest) {
			return ErrDocumentSaveInDoubt
		}
		if err := linkDocumentExclusive(parent, backup, backupName, intent.Candidate); err != nil {
			return ErrDocumentSaveInDoubt
		}
	} else if err != nil {
		return ErrDocumentSaveInDoubt
	} else if !matchesWindowsSwapFile(parent, targetName, intent.CandidateIdentity, intent.CandidateDigest) ||
		!matchesWindowsSwapFile(parent, intent.Candidate, intent.OriginalIdentity, intent.ExpectedDigest) {
		return ErrDocumentSaveInDoubt
	}
	if err := parent.Remove(backupName); err != nil {
		return ErrDocumentSaveInDoubt
	}
	return nil
}

func matchesWindowsSwapFile(parent *os.Root, name string, identity ProjectIdentity, digest string) bool {
	file, err := openRegularDocument(parent, name)
	if err != nil {
		return false
	}
	defer file.Close()
	actual, err := identityOfFile(file)
	if err != nil || actual != identity {
		return false
	}
	data, err := readBoundedDocument(file)
	return err == nil && digestBytes(data) == digest
}

func reconcileSwapArtifacts(parent *os.Root, targetName string, intent saveIntent) error {
	return reconcileWindowsSwap(parent, targetName, intent)
}
