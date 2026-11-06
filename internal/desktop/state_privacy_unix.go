//go:build darwin || linux

package desktop

import "os"

func createStateDirectory(dir string) error { return os.MkdirAll(dir, 0700) }

func privateStateDirectory(file *os.File) error {
	info, err := file.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrDocumentSaveUnavailable
	}
	return nil
}

func privateStateRegular(file *os.File) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return ErrDocumentSaveUnavailable
	}
	return nil
}
