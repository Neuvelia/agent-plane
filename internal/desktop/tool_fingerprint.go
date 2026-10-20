package desktop

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"strconv"
)

const maxToolFingerprintBytes int64 = 256 << 20

// fingerprintTool binds a configured executable's current regular file,
// identity and bytes. Path-based process creation can still race replacement
// after this check; Start repeats it immediately before scheduling a run.
func fingerprintTool(path string) (string, error) {
	canonical, err := validateToolPath(path)
	if err != nil || canonical != path {
		return "", ErrToolUnavailable
	}
	file, err := os.Open(canonical)
	if err != nil {
		return "", ErrToolUnavailable
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > maxToolFingerprintBytes {
		return "", ErrToolUnavailable
	}
	identity, err := identityOfFile(file)
	if err != nil {
		return "", ErrToolUnavailable
	}
	hash := sha256.New()
	for _, field := range []string{"agentplane.tool.v1", canonical, string(identity),
		strconv.FormatInt(before.Size(), 10), strconv.FormatInt(before.ModTime().UnixNano(), 10)} {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		_, _ = hash.Write(size[:])
		_, _ = io.WriteString(hash, field)
	}
	if _, err := io.CopyN(hash, file, before.Size()); err != nil {
		return "", ErrToolUnavailable
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || after.Size() != before.Size() ||
		!after.ModTime().Equal(before.ModTime()) {
		return "", ErrToolUnavailable
	}
	afterIdentity, err := identityOfFile(file)
	if err != nil || afterIdentity != identity {
		return "", ErrToolUnavailable
	}
	pathInfo, err := os.Stat(canonical)
	if err != nil || !pathInfo.Mode().IsRegular() || !os.SameFile(pathInfo, after) {
		return "", ErrToolUnavailable
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
