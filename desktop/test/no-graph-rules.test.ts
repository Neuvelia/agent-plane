import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// F2.1: no graph rule lives in TypeScript. Whether two ports connect, which
// nodes are valid or what an edge means is always the engine's answer. This
// scans the renderer sources for the validator's diagnostic codes and for
// port-type comparison; stories and fixtures are recordings and are skipped.
const files = (dir: string): string[] =>
  readdirSync(dir).flatMap((n) => {
    const p = join(dir, n)
    if (statSync(p).isDirectory()) return files(p)
    return /\.(ts|tsx)$/.test(n) && !/\.stories\.tsx$/.test(n) ? [p] : []
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
})
