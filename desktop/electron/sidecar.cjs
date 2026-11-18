'use strict'
// Locates the Go engine binary. Pure: platform, environment and the existence
// check are injected so the Windows branch is testable on any host.
const path = require('node:path')

function binaryName(platform) {
  return platform === 'win32' ? 'agentplane.exe' : 'agentplane'
}

function resolveSidecar({ platform, repoRoot, env, exists }) {
  const override = env.AGENTPLANE_BIN
  const source = override ? 'AGENTPLANE_BIN' : 'repo'
  const binary = override || path.join(repoRoot, 'bin', binaryName(platform))
  if (exists(binary)) return { path: binary, source }
  const origin = override ? 'AGENTPLANE_BIN points to it' : 'default location'
  return {
    path: binary,
    source,
    error: `engine binary not found at ${binary} (${origin}). Run "make build" to create bin/${binaryName(platform)}, or set AGENTPLANE_BIN to an existing agentplane binary.`,
  }
}

module.exports = { binaryName, resolveSidecar }
