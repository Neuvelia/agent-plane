import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// F2.1: no graph rule lives in TypeScript. Whether two ports connect, which
// nodes are valid or what an edge means is always the engine's answer. This
// scans the renderer sources for the validator's diagnostic codes and for
// port-type comparison; stories, fixtures and the Storybook bridge are recordings and are skipped.
const files = (dir: string): string[] =>
  readdirSync(dir).flatMap((n) => {
    const p = join(dir, n)
    if (statSync(p).isDirectory()) return files(p)
    return /\.(ts|tsx)$/.test(n) && !/\.stories\.tsx$/.test(n) && !/[\\/]storybook[\\/]/.test(p) ? [p] : []
  })

describe('no graph rules in the renderer', () => {
  const sources = files(join(__dirname, '..', 'src')).map((p) => ({ p, text: readFileSync(p, 'utf8') }))

  it('never names a validator diagnostic code', () => {
    const hits = sources.filter((s) => /['"`][EW]_[A-Z][A-Z_]{4,}['"`]/.test(s.text)).map((s) => s.p)
    expect(hits).toEqual([])
  })

  it('never compares port types or decides connectability', () => {
    const hits = sources.filter((s) => /\.type\s*[!=]==?\s*[a-z]+\.type\b|canConnect\s*=|isCompatible|typesMatch/.test(s.text)).map((s) => s.p)
    expect(hits).toEqual([])
  })

  // F2.12: the template picker copies bundled text and replaces one `id:` line. It
  // never parses template YAML, names a node type to decide anything or lists a config key.
  const picker = sources.filter((s) => /[\\/](templates\.ts|TemplatePicker\.tsx)$/.test(s.p))
  it('scans the template picker', () => {
    expect(picker.map((s) => s.p.split(/[\\/]/).pop()).sort()).toEqual(['TemplatePicker.tsx', 'templates.ts'])
  })
  it('the template picker never parses YAML or branches on a node type or config key', () => {
    const hits = picker.filter((s) => /from ['"](yaml|js-yaml|@codemirror\/lang-yaml)['"]|YAML\.parse|JSON\.parse|\.type\s*[!=]==?\s*['"`]|config\??\.\w+|workspace|allowed_paths/.test(s.text)).map((s) => s.p)
    expect(hits).toEqual([])
  })

  // F2.3: the bindings files read the engine's answers (list_connections, diagnostics, the batch itself).
  // They may map references to operations, but never compare port types, name a validator code or search for a cycle.
  const bindingFiles = sources.filter((s) => /[\\/]editor[\\/](bindings|connections|selection)\.ts$|[\\/]editor[\\/](BindingsPanel|AddBindingForm|TargetOptions)\.tsx$/.test(s.p))
  it('scans the bindings panel files', () => {
    expect(bindingFiles.map((s) => s.p.split(/[\\/]/).pop()).sort()).toEqual(['AddBindingForm.tsx', 'BindingsPanel.tsx', 'TargetOptions.tsx', 'bindings.ts', 'connections.ts', 'selection.ts'])
  })
  const code = (text: string) => {
    const bare = text.replace(/^\s*\/\/.*$/gm, '').replace(/\/\*\*[\s\S]*?\*\//g, '')
    return /\.type\s*[!=]==?|\btype\s*[!=]==?\s*\w+\.type|[EW]_[A-Z]{3,}|\.code\s*[!=]==?/.test(bare) || /cycle/i.test(bare)
  }
  it('the bindings files never compare types, name a code or look for cycles', () => {
    const hits = bindingFiles.filter((s) => code(s.text)).map((s) => s.p)
    expect(hits).toEqual([])
  })

  // F2.2: the inspector renders the sidecar's descriptors. It must not branch on
  // a node type or name a config key to decide what is valid.
  const inspector = sources.filter((s) => s.p.includes(join('editor', 'inspector')))
  it('scans the inspector', () => {
    expect(inspector.length).toBeGreaterThanOrEqual(5)
  })
  it('the inspector never branches on a node type', () => {
    const hits = inspector.filter((s) => /\.type\s*[!=]==?\s*['"`](shell|agent|gate|human)['"`]|case\s+['"`](shell|agent|gate|human)['"`]/.test(s.text)).map((s) => s.p)
    expect(hits).toEqual([])
  })
  // fieldText.ts is the help catalogue, keyed by descriptor ids such as shell.config.command.
  it('the inspector never checks a config value by key', () => {
    const hits = inspector.filter((s) => !s.p.endsWith('fieldText.ts')).filter((s) => /config\??\.(command|adapter|workspace|task|kind|prompt|check_id|allowed_paths)\b|config\[['"`][a-z_]+['"`]\]/.test(s.text)).map((s) => s.p)
    expect(hits).toEqual([])
  })
})
