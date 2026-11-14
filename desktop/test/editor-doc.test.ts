import { describe, expect, it } from 'vitest'
import { COALESCE_MS, MAX_HISTORY, analyzed, counts, edited, fromNew, fromOpen, isDirty, newGraphText, redo, reloaded, saved, type, undo, validating, withConflict } from '../src/editor/doc'
import type { OpenResponse } from '../src/editor/types'

const open = (text = 'a\n'): OpenResponse => ({ text, hash: 'h0', version: 'v0', diagnostics: [], readonly_canvas: false, line_ending: 'lf', label: 'g.yaml' })
const start = (text = 'a\n') => fromOpen('grant', open(text))

describe('document state', () => {
  it('starts clean from an opened file', () => {
    const s = start()
    expect(isDirty(s)).toBe(false)
    expect(s).toMatchObject({ draft: 'a\n', savedText: 'a\n', savedHash: 'h0', isNew: false, past: [], future: [] })
  })

  it('a new graph is dirty-by-definition until its first save', () => {
    const s = fromNew('g', 'new.yaml', 'x\n')
    expect(s).toMatchObject({ isNew: true, savedHash: null, savedText: '' })
    expect(isDirty(s)).toBe(true)
    const done = saved(s, 'x\n', 'h1')
    expect(done).toMatchObject({ isNew: false, savedHash: 'h1' })
    expect(isDirty(done)).toBe(false)
  })

  it('typing marks the draft dirty and the analysis stale', () => {
    const s = type(start(), 'ab\n', 1000)
    expect(isDirty(s)).toBe(true)
    expect(s.analysis.stale).toBe(true)
  })

  it('typing the same text again changes nothing', () => {
    const s = start()
    expect(type(s, 'a\n', 5)).toBe(s)
  })

  it('typing close together is one undo step; a pause starts a new one', () => {
    let s = start('')
    s = type(s, 'a', 1000)
    s = type(s, 'ab', 1100)
    s = type(s, 'abc', 1100 + COALESCE_MS)
    expect(s.past).toEqual([''])
    s = type(s, 'abcd', 1100 + COALESCE_MS + COALESCE_MS + 1)
    expect(s.past).toEqual(['', 'abc'])
  })

  it('undo and redo restore the exact previous texts', () => {
    let s = start('')
    for (const [i, t] of ['one', 'two', 'three'].entries()) s = type(s, t, 10_000 * (i + 1))
    expect(s.draft).toBe('three')
    s = undo(s)
    expect(s.draft).toBe('two')
    s = undo(s)
    expect(s.draft).toBe('one')
    s = redo(s)
    expect(s.draft).toBe('two')
    s = redo(s)
    expect(s.draft).toBe('three')
    expect(redo(s)).toBe(s)
    expect(undo(undo(undo(undo(s))))).toMatchObject({ draft: '', past: [] })
  })

  it('a new edit clears the redo stack', () => {
    let s = start('')
    s = type(s, 'one', 10_000)
    s = type(s, 'two', 20_000)
    s = undo(s)
    expect(s.future).toEqual(['two'])
    s = type(s, 'other', 30_000)
    expect(s.future).toEqual([])
    expect(redo(s)).toBe(s)
  })

  it('undoing past the saved text makes it dirty again, and back to it clean', () => {
    let s = start('base')
    s = type(s, 'edit', 10_000)
    expect(isDirty(s)).toBe(true)
    s = undo(s)
    expect(s.draft).toBe('base')
    expect(isDirty(s)).toBe(false)
  })

  it('keeps at most MAX_HISTORY undo steps', () => {
    let s = start('')
    for (let i = 1; i <= MAX_HISTORY + 50; i++) s = type(s, String(i), i * 10_000)
    expect(s.past).toHaveLength(MAX_HISTORY)
    expect(s.past[0]).toBe('50')
    for (let i = 0; i < MAX_HISTORY + 10; i++) s = undo(s)
    expect(s.draft).toBe('50')
  })

  it('an undo or redo ends the typing run, so the next typing is a new step', () => {
    let s = start('')
    s = type(s, 'a', 10_000)
    s = type(s, 'ab', 20_000)
    s = undo(s)
    const t = type(s, 'ax', 20_050)
    expect(t.past).toEqual(['', 'a'].slice(0, t.past.length))
    expect(t.past.at(-1)).toBe('a')
  })

  it('saving keeps what was typed after the text that was sent', () => {
    let s = start('a')
    s = type(s, 'sent', 10_000)
    s = type(s, 'sent plus more', 10_100)
    s = saved(s, 'sent', 'h2')
    expect(s).toMatchObject({ savedText: 'sent', savedHash: 'h2', draft: 'sent plus more' })
    expect(isDirty(s)).toBe(true)
  })

  it('saving clears a conflict; reload replaces the draft and the history', () => {
    let s = withConflict(type(start('a'), 'b', 10_000), { kind: 'changed', currentHash: 'h9' })
    expect(s.conflict).toEqual({ kind: 'changed', currentHash: 'h9' })
    expect(saved(s, 'b', 'h10').conflict).toBeUndefined()
    s = reloaded(s, { ...open('disk\n'), hash: 'h9' })
    expect(s).toMatchObject({ draft: 'disk\n', savedText: 'disk\n', savedHash: 'h9', past: [], future: [] })
    expect(s.conflict).toBeUndefined()
  })

  it('a validation result for older text is ignored', () => {
    let s = type(start('a'), 'b', 10_000)
    s = validating(s)
    expect(s.analysis.status).toBe('validating')
    const ignored = analyzed(s, 'a', { status: 'invalid', diagnostics: [{ severity: 'error', code: 'E_X', message: 'old' }] })
    expect(ignored).toBe(s)
    const applied = analyzed(s, 'b', { status: 'valid', diagnostics: [], version: 'v1' })
    expect(applied.analysis).toMatchObject({ status: 'ready', stale: false, version: 'v1' })
  })

  it('counts errors and warnings', () => {
    expect(counts({ status: 'ready', stale: false, diagnostics: [{ severity: 'error', code: 'a', message: '' }, { severity: 'warning', code: 'b', message: '' }, { severity: 'warning', code: 'c', message: '' }] })).toEqual({ errors: 1, warnings: 2 })
  })
})

describe('newGraphText', () => {
  it('derives a valid graph id from the file name', () => {
    const idOf = (label: string) => /\nid: (.*)\n/.exec(newGraphText(label))![1]
    expect(idOf('fix-issue.yaml')).toBe('fix-issue')
    expect(idOf('My Graph.YML')).toBe('my-graph')
    expect(idOf('9lives.yaml')).toBe('lives')
    expect(idOf('Żółć.yaml')).toBe('graph')
    expect(idOf('---.yaml')).toBe('graph')
    expect(idOf('a_b-c1.yaml')).toBe('a_b-c1')
  })

  it('is schema v0 with empty collections', () => {
    expect(newGraphText('x.yaml')).toBe('schema_version: 0\nid: x\ninputs: {}\nnodes: {}\nedges: []\noutputs: {}\n')
  })
})

describe('operations (canvas edits)', () => {
  const defn = { id: 'g', nodes: {}, edges: [], outputs: {} }
  const after = (text: string, definition = defn): OpenResponse => ({ ...open(text), definition })

  it('an operation is its own undo step, clears redo, and carries the engine answer', () => {
    let s = start('a\n')
    s = type(s, 'b\n', 1000)
    s = undo(s)
    expect(s.future).toEqual(['b\n'])
    s = edited(s, after('c\n'))
    expect(s.draft).toBe('c\n')
    expect(s.past).toEqual(['a\n'])
    expect(s.future).toEqual([])
    expect(s.analysis.stale).toBe(false)
    expect(s.analysis.definition).toBe(defn)
    expect(undo(s).draft).toBe('a\n')
  })

  it('typing right after an operation is a separate step, never merged into it', () => {
    let s = edited(start('a\n'), after('b\n'))
    s = type(s, 'b!\n', 5000)
    expect(undo(s).draft).toBe('b\n')
  })

  it('an answer that changes nothing adds no history', () => {
    const s = start('a\n')
    expect(edited(s, after('a\n'))).toBe(s)
  })

  it('keeps the last good definition while the text does not parse, and drops it for a valid one', () => {
    let s = edited(start('a\n'), after('b\n'))
    s = type(s, 'b [\n', 5000)
    s = analyzed(s, 'b [\n', { status: 'invalid', diagnostics: [], parse_error: 'bad' })
    expect(s.analysis.parseError).toBe('bad')
    expect(s.analysis.definition).toBe(defn)
    const next = { ...defn, id: 'h' }
    s = type(s, 'ok\n', 9000)
    s = analyzed(s, 'ok\n', { status: 'valid', diagnostics: [], definition: next })
    expect(s.analysis.definition).toBe(next)
    expect(s.analysis.parseError).toBeUndefined()
  })

  it('reports read-only reasons from the engine', () => {
    const s = analyzed(type(start(), 'x\n', 5000), 'x\n', { status: 'valid', diagnostics: [], readonly_reasons: ['anchors'] })
    expect(s.analysis.readonlyReasons).toEqual(['anchors'])
  })
})
