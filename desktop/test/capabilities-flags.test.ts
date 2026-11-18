import { describe, expect, it } from 'vitest'
import { codexBinFlags } from '../electron/capabilities.cjs'

// F2.11 requirement 4/8: main passes --codex-bin only for an absolute AGENTPLANE_CODEX_BIN.
describe('serve --codex-bin from AGENTPLANE_CODEX_BIN', () => {
  it('passes an absolute POSIX path', () => {
    expect(codexBinFlags({ AGENTPLANE_CODEX_BIN: '/opt/codex/bin/codex' }, 'linux')).toEqual(['--codex-bin', '/opt/codex/bin/codex'])
  })
  it('passes an absolute Windows path under the Windows rules', () => {
    expect(codexBinFlags({ AGENTPLANE_CODEX_BIN: 'C:\\Tools\\codex.exe' }, 'win32')).toEqual(['--codex-bin', 'C:\\Tools\\codex.exe'])
  })
  it('passes a UNC path on Windows', () => {
    expect(codexBinFlags({ AGENTPLANE_CODEX_BIN: '\\\\host\\share\\codex.exe' }, 'win32')).toEqual(['--codex-bin', '\\\\host\\share\\codex.exe'])
  })
  it('refuses a rooted Windows path without a drive: Go does not treat it as absolute', () => {
    for (const value of ['\\tools\\codex.exe', '/tools/codex.exe', 'C:codex.exe', '\\\\host']) {
      expect(codexBinFlags({ AGENTPLANE_CODEX_BIN: value }, 'win32'), value).toEqual([])
    }
  })
  it('passes nothing when unset, empty, relative or carrying a NUL', () => {
    for (const env of [{}, { AGENTPLANE_CODEX_BIN: undefined }, { AGENTPLANE_CODEX_BIN: '' }, { AGENTPLANE_CODEX_BIN: 'codex' }, { AGENTPLANE_CODEX_BIN: './bin/codex' }, { AGENTPLANE_CODEX_BIN: '../codex' }, { AGENTPLANE_CODEX_BIN: '/opt/co\0dex' }]) {
      expect(codexBinFlags(env, 'linux'), JSON.stringify(env)).toEqual([])
    }
  })
  it('ignores an inherited property that is not an own environment entry', () => {
    const env = Object.create({ AGENTPLANE_CODEX_BIN: '/opt/codex' }) as Record<string, string>
    expect(codexBinFlags(env, 'linux')).toEqual([])
  })
})
