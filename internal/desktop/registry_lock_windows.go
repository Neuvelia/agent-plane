//go:build windows

package desktop

// A Windows lock also needs a verified private DACL on the state directory and
// lock file. A native LockFileEx implementation belongs to Task 10 together
// with those ACL and reparse-point checks. Until then registration fails closed.
func acquireRegistryLock(string) (registryLock, error) {
	return nil, ErrRegistryLockUnavailable
}
