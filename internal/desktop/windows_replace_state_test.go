package desktop

import "testing"

func TestWindowsReplaceFailureKeepsDisplacedFileRecoverable(t *testing.T) {
	for _, test := range []struct {
		code uint32
		want windowsReplaceState
	}{
		{0, windowsReplaceComplete},
		{1175, windowsReplaceNamesUnchanged},
		{1176, windowsReplaceNamesUnchanged},
		{1177, windowsReplaceOriginalAtBackup},
		{5, windowsReplaceUnknown},
	} {
		if got := classifyWindowsReplace(test.code); got != test.want {
			t.Errorf("ReplaceFileW error %d: state %d, want %d", test.code, got, test.want)
		}
	}
}
