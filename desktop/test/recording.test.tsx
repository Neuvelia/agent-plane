import { mkdirSync, mkdtempSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { MAX_NAMES, createRecordingSettings, serveFlags } from '../electron/recording.cjs'
import { I18nProvider } from '../src/i18n/context'
import { ThemeProvider } from '../src/theme'
import { Settings, parseNames } from '../src/settings'

function store() {
  const file = join(mkdtempSync(join(tmpdir(), 'ap-recording-')), 'sub', 'recording.json')
  return { file, s: createRecordingSettings(file) }
}

describe('record run values preference (main process)', () => {
  it('is off by default, and stays off for missing or corrupt storage', () => {
    expect(store().s.load()).toEqual({ enabled: false, redactEnv: [] })
    for (const bad of ['not json', '{"version":2,"enabled":true,"redactEnv":[]}', '{"version":1,"enabled":"yes","redactEnv":[]}', '{"version":1,"enabled":true,"redactEnv":["A B"]}']) {
      const { s, file } = store()
      mkdirSync(dirname(file), { recursive: true })
      writeFileSync(file, bad)
      expect(s.load(), bad).toEqual({ enabled: false, redactEnv: [] })
    }
  })

  it('saves a valid preference with owner-only permissions and reads it back', () => {
    const { s, file } = store()
    expect(s.save({ enabled: true, redactEnv: ['GITHUB_TOKEN', 'GITHUB_TOKEN', '_X1'] })).toEqual({ ok: true, settings: { enabled: true, redactEnv: ['GITHUB_TOKEN', '_X1'] } })
    expect(s.load()).toEqual({ enabled: true, redactEnv: ['GITHUB_TOKEN', '_X1'] })
    expect(JSON.parse(readFileSync(file, 'utf8')).version).toBe(1)
    if (process.platform !== 'win32') expect(statSync(file).mode & 0o777).toBe(0o600)
  })

  it('rejects anything but a boolean switch and plain variable names', () => {
    const { s } = store()
    const rejected: unknown[] = [
      null,
      [],
      { enabled: 'true', redactEnv: [] },
      { enabled: true },
      { enabled: true, redactEnv: [], extra: 1 },
      { enabled: true, redactEnv: ['A=B'] },
      { enabled: true, redactEnv: ['--record-values'] },
      { enabled: true, redactEnv: ['1ABC'] },
      { enabled: true, redactEnv: [''] },
      { enabled: true, redactEnv: [7] },
      { enabled: true, redactEnv: Array.from({ length: MAX_NAMES + 1 }, (_, i) => `V${i}`) },
    ]
    for (const input of rejected) expect(s.save(input), JSON.stringify(input)).toMatchObject({ ok: false, code: 'invalid' })
    expect(s.save({ enabled: true, redactEnv: ['A=B'] })).toEqual({ ok: false, code: 'invalid', name: 'A=B' })
    expect(s.load()).toEqual({ enabled: false, redactEnv: [] })
  })

  it('reports a write failure instead of claiming the setting was saved', () => {
    const dir = mkdtempSync(join(tmpdir(), 'ap-recording-'))
    const blocker = join(dir, 'file')
    writeFileSync(blocker, 'x')
    expect(createRecordingSettings(join(blocker, 'recording.json')).save({ enabled: true, redactEnv: [] })).toEqual({ ok: false, code: 'save_failed' })
  })
})

describe('serve flags', () => {
  it('passes nothing when recording is off and no names are configured (the default)', () => {
    expect(serveFlags({ enabled: false, redactEnv: [] }, { HOME: '/h' })).toEqual({ args: [], recording: false, missing: [] })
  })
  it('passes --redact-env for set names and --record-values only when enabled', () => {
    expect(serveFlags({ enabled: true, redactEnv: ['A', 'B'] }, { A: 'secret', B: '' })).toEqual({ args: ['--redact-env', 'A', '--redact-env', 'B', '--record-values'], recording: true, missing: [] })
    expect(serveFlags({ enabled: false, redactEnv: ['A'] }, { A: 'secret' })).toEqual({ args: ['--redact-env', 'A'], recording: false, missing: [] })
  })
  it('keeps recording off and never passes an unset name, so serve still starts', () => {
    const flags = serveFlags({ enabled: true, redactEnv: ['A', 'GONE'] }, { A: 'secret' })
    expect(flags).toEqual({ args: ['--redact-env', 'A'], recording: false, missing: ['GONE'] })
  })
  it('reports unset names only when recording is enabled', () => {
    expect(serveFlags({ enabled: false, redactEnv: ['A', 'GONE'] }, { A: 'secret' })).toEqual({ args: ['--redact-env', 'A'], recording: false, missing: [] })
  })
  it('never puts an environment value into the arguments', () => {
    expect(JSON.stringify(serveFlags({ enabled: true, redactEnv: ['A'] }, { A: 'hunter2' }))).not.toContain('hunter2')
  })
})

describe('Settings: record run values', () => {
  const html = (locale: 'en' | 'pl') =>
    renderToStaticMarkup(
      <I18nProvider locale={locale}>
        <ThemeProvider>
          <Settings />
        </ThemeProvider>
      </I18nProvider>,
    )
  it('is localised', () => {
    const out = html('pl')
    expect(out).toContain('Zapisuj wartości przebiegów')
    expect(out).not.toContain('Record run values')
  })
  it('splits typed names on spaces and commas', () => {
    expect(parseNames(' A, B  C,,')).toEqual(['A', 'B', 'C'])
    expect(parseNames('')).toEqual([])
  })
})
