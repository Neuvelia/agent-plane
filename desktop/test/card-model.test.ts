import { describe, expect, it } from 'vitest'
import fixIssueError from './fixtures/graph-open-editor-v2-error.json'
import fixIssue from './fixtures/graph-open-editor-v2.json'
import { cardModel } from '../src/editor/cardModel'
import { badgesOf } from '../src/editor/issues'
import type { OpenResponse } from '../src/editor/types'

// The step cards of the graph editor v2 summarise a step from the engine's own
// answer: its definition, field descriptors and diagnostics (recorded fixtures).

const model = (open: unknown, id: string) => {
  const o = open as OpenResponse
  const def = o.definition!
  return cardModel(def, id, o.node_types?.[def.nodes[id]!.type], badgesOf(o.diagnostics, def))
}

describe('step card model', () => {
  it('shows the prompt, path lists before required values, and at most two settings [F2.1b-AC2]', () => {
    const fix = model(fixIssue, 'fix')
    expect(fix.prompt).toBe('Fix the issue below. Change only files under src/. Run the tests before you finish.')
    expect(fix.settings.map((s) => [s.key, s.value])).toEqual([
      ['allowed_paths', 'src/'],
      ['adapter', 'codex'],
    ])
    expect(fix.retries).toBe(3)
  })

  it('says where each input comes from [F2.1b-AC2]', () => {
    expect(model(fixIssue, 'fix').inputs).toEqual([{ name: 'issue', required: true, source: { kind: 'node', node: 'read-issue', port: 'issue' }, error: false }])
    expect(model(fixIssue, 'review').inputs.map((i) => i.source)).toEqual([
      { kind: 'node', node: 'fix', port: 'summary' },
      { kind: 'node', node: 'tests', port: 'verdict' },
    ])
  })

  it('marks an unconnected input with the engine error, and only then', () => {
    const fix = model(fixIssueError, 'fix')
    expect(fix.inputs).toEqual([{ name: 'issue', required: true, source: { kind: 'none' }, error: true }])
  })

  it('marks an output the engine warns about', () => {
    const outs = model(fixIssue, 'read-issue').outputs
    expect(outs.find((o) => o.name === 'log')).toMatchObject({ warning: true, error: false })
    expect(outs.find((o) => o.name === 'issue')).toMatchObject({ warning: false, error: false })
  })

  it('without descriptors it still lists ports, and invents no settings', () => {
    const o = fixIssue as unknown as OpenResponse
    const m = cardModel(o.definition!, 'fix', undefined, badgesOf([], o.definition))
    expect(m.settings).toEqual([])
    expect(m.prompt).toBeUndefined()
    expect(m.outputs.map((p) => p.name).sort()).toEqual(['changed_files', 'summary'])
  })

  it('an unknown node id yields an empty card', () => {
    const o = fixIssue as unknown as OpenResponse
    expect(cardModel(o.definition!, 'nope', undefined, badgesOf([], o.definition))).toEqual({ settings: [], inputs: [], outputs: [] })
  })
})
