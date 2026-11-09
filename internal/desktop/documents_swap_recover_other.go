//go:build !windows

package desktop

import "os"

func reconcileSwapArtifacts(_ *os.Root, _ string, _ saveIntent) error { return nil }
