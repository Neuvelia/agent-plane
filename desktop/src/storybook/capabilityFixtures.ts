import recorded from '../../test/fixtures/capabilities-linux.json'
import type { CapabilityReport, CapabilityTool } from '../capabilities'

// F2.11 capability reports for stories. `linux` is recorded from a real
// `agentplane serve` (GET /capabilities on Linux, no AGENTPLANE_CODEX_BIN). The
// others change only row fields to statuses and reasons the engine produces
// (internal/capability tests pin each mapping), so every screen state can be shown.
export const linux = recorded as CapabilityReport

const withRows = (base: CapabilityReport, rows: Record<string, Partial<CapabilityTool>>, engine: Partial<CapabilityReport['engine']> = {}): CapabilityReport => ({
  ...base,
  engine: { ...base.engine, ...engine },
  tools: base.tools.map((t) => ({ ...t, ...(rows[t.id] ?? {}) })),
})

export const allAvailable = withRows(linux, { codex: { status: 'available', version: '0.163.0', reason: '' }, claude: { status: 'detected_no_adapter' }, 'cursor-agent': { status: 'detected_no_adapter' }, gemini: { status: 'detected_no_adapter' } })
export const gitMissing = withRows(linux, { git: { status: 'missing', version: '' } })
export const codexFoundOnPath = withRows(linux, { codex: { reason: 'found_on_path' } })
export const clisFound = withRows(linux, { claude: { status: 'detected_no_adapter' }, gemini: { status: 'detected_no_adapter' } })
export const codexUnsupportedVersion = withRows(linux, { codex: { status: 'unsupported_version', reason: 'missing_required_flags' } })
export const windows = withRows(
  linux,
  { git: { status: 'unsupported_platform', version: '', reason: 'platform_not_supported' }, codex: { status: 'unsupported_platform', reason: 'platform_not_supported' }, claude: { status: 'detected_no_adapter' } },
  { os: 'windows', arch: 'amd64' },
)
export const detectionError = withRows(linux, { git: { status: 'error', version: '', reason: 'probe_timeout' }, codex: { status: 'error', reason: 'probe_failed' } })
