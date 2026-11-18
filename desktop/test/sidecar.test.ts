import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { binaryName, resolveSidecar } from '../electron/sidecar.cjs'

const repo = join('repo')

describe('sidecar binary resolution', () => {
  it('names the binary per platform', () => {
    expect(binaryName('win32')).toBe('agentplane.exe')
    expect(binaryName('linux')).toBe('agentplane')
    expect(binaryName('darwin')).toBe('agentplane')
  })

  it('looks for bin/agentplane.exe on Windows', () => {
    const r = resolveSidecar({ platform: 'win32', repoRoot: repo, env: {}, exists: () => true })
    expect(r.path).toBe(join(repo, 'bin', 'agentplane.exe'))
    expect(r.source).toBe('repo')
    expect(r.error).toBeUndefined()
  })

  it('looks for bin/agentplane on Linux and macOS', () => {
    for (const platform of ['linux', 'darwin']) {
      const r = resolveSidecar({ platform, repoRoot: repo, env: {}, exists: () => true })
      expect(r.path).toBe(join(repo, 'bin', 'agentplane'))
    }
  })

  it('lets AGENTPLANE_BIN override the default without probing the default', () => {
    const probed: string[] = []
    const r = resolveSidecar({
      platform: 'win32',
      repoRoot: repo,
      env: { AGENTPLANE_BIN: 'custom/engine.exe' },
      exists: (p: string) => (probed.push(p), true),
    })
    expect(r.path).toBe('custom/engine.exe')
    expect(r.source).toBe('AGENTPLANE_BIN')
    expect(probed).toEqual(['custom/engine.exe'])
  })

  it('ignores an empty AGENTPLANE_BIN', () => {
    const r = resolveSidecar({ platform: 'linux', repoRoot: repo, env: { AGENTPLANE_BIN: '' }, exists: () => true })
    expect(r.source).toBe('repo')
  })

  it('reports the tried path and how to build when the binary is missing', () => {
    const r = resolveSidecar({ platform: 'win32', repoRoot: repo, env: {}, exists: () => false })
    expect(r.path).toBe(join(repo, 'bin', 'agentplane.exe'))
    expect(r.error).toContain(join(repo, 'bin', 'agentplane.exe'))
    expect(r.error).toContain('make build')
    expect(r.error).toContain('AGENTPLANE_BIN')
  })

  it('names the override path when the override is missing', () => {
    const r = resolveSidecar({ platform: 'linux', repoRoot: repo, env: { AGENTPLANE_BIN: '/nope/ap' }, exists: () => false })
    expect(r.error).toContain('/nope/ap')
    expect(r.error).toContain('AGENTPLANE_BIN')
  })
})
