'use strict'
// F2.11 (ADR-0025): the only input Electron main gives the engine's capability
// detection. Codex is detected only from an absolute AGENTPLANE_CODEX_BIN; any
// other value (unset, empty, relative, containing a NUL) passes nothing, so
// `serve` reports Codex as not configured instead of refusing to start. The
// value is never logged and never reaches the renderer.
const path = require('node:path')

// Mirrors Go's filepath.IsAbs: on Windows only a drive-letter or UNC path counts, so a
// rooted `\tools\codex.exe` (absolute to Node, not to the engine) is refused here.
const WINDOWS_ABSOLUTE = /^(?:[A-Za-z]:[\\/]|\\\\[^\\/]+[\\/]+[^\\/]+)/

function isAbsolutePath(value, platform) {
  return platform === 'win32' ? WINDOWS_ABSOLUTE.test(value) : path.posix.isAbsolute(value)
}

function codexBinFlags(env, platform = process.platform) {
  const value = env && Object.prototype.hasOwnProperty.call(env, 'AGENTPLANE_CODEX_BIN') ? env.AGENTPLANE_CODEX_BIN : undefined
  if (typeof value !== 'string' || value === '' || value.includes('\0') || !isAbsolutePath(value, platform)) return []
  return ['--codex-bin', value]
}

module.exports = { codexBinFlags }
