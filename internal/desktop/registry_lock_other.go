//go:build !darwin && !linux && !windows

package desktop

func acquireRegistryLock(string) (registryLock, error) {
	return nil, ErrRegistryLockUnavailable
}
