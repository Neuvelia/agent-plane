//go:build !darwin && !linux && !windows

package desktop

import "os"

func createStateDirectory(string) error { return ErrDocumentSaveUnavailable }

func privateStateDirectory(*os.File) error { return ErrDocumentSaveUnavailable }
func privateStateRegular(*os.File) error   { return ErrDocumentSaveUnavailable }
