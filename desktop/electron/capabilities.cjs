'use strict'
// F2.11 (ADR-0025): the only input Electron main gives the engine's capability
// detection. Codex is detected only from an absolute AGENTPLANE_CODEX_BIN; any
// other value (unset, empty, relative, containing a NUL) passes nothing, so
// `serve` reports Codex as not configured instead of refusing to start. The
// value is never logged and never reaches the renderer.
const path = require('node:path')

function codexBinFlags(env, isAbsolute = path.isAbsolute) {
  const value = env && Object.prototype.hasOwnProperty.call(env, 'AGENTPLANE_CODEX_BIN') ? env.AGENTPLANE_CODEX_BIN : undefined
  if (typeof value !== 'string' || value === '' || value.includes('\0') || !isAbsolute(value)) return []
  return ['--codex-bin', value]
}

module.exports = { codexBinFlags }
