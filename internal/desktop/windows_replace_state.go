package desktop

// ReplaceFileW has documented partial outcomes. Keep this classification
// independent of the Windows call so recovery never treats 1177 as unchanged.
type windowsReplaceState uint8

const (
	windowsReplaceUnknown windowsReplaceState = iota
	windowsReplaceComplete
	windowsReplaceNamesUnchanged
	windowsReplaceOriginalAtBackup
)

func classifyWindowsReplace(code uint32) windowsReplaceState {
	switch code {
	case 0:
		return windowsReplaceComplete
	case 1175, 1176:
		return windowsReplaceNamesUnchanged
	case 1177:
		return windowsReplaceOriginalAtBackup
	default:
		return windowsReplaceUnknown
	}
}
