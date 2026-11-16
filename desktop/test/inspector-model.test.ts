import { describe, expect, it } from 'vitest'
import loops from './fixtures/inspector-open-loops.json'
import shell from './fixtures/inspector-open-shell.json'
import types from './fixtures/inspector-open-types.json'
import removePort from './fixtures/inspector-edit-remove-port.json'
import nodeTypes from './fixtures/node-types.json'
import { activeFields, configOp, describeChange, diagnosticsAt, diagnosticsExactly, holds, inputSources, keyTaken, loopAttemptsOp, loopConfigOp, loopStarter, nodeDiagnostics, parseInteger, parseLoose, unshown } from '../src/editor/inspector/model'
import type { Definition, NodeTypeDescriptor, OpenResponse } from '../src/editor/types'

// F2.2 renderer unit tests: field-to-operation mapping, diagnostic-to-field
// matching, input sources and port-edit previews, over recorded API answers.
const cat = nodeTypes as unknown as Record<string, NodeTypeDescriptor>
const def = (r: unknown) => (r as OpenResponse).definition as Definition

describe('descriptor selection', () => {
  it('shows only the fields of the selected Gate kind', () => {
    const keys = (kind: string) => activeFields(cat.gate!.config, { kind }).map((f) => f.key)
    expect(keys('command')).toEqual(['check_id', 'kind', 'command', 'cwd', 'left_input', 'right_input'])
    expect(keys('json_predicate')).toEqual(['check_id', 'kind', 'input', 'field', 'operator', 'expected', 'expected_json'])
    expect(keys('nope')).toEqual(['check_id', 'kind'])
  })
  it('a standalone Gate offers three kinds, an agent loop four', () => {
    expect(cat.gate!.config.find((f) => f.key === 'kind')!.enum).toEqual(['command', 'json_predicate', 'reported_file_scope'])
    expect(cat.agent!.loop!.gate_kinds).toHaveLength(4)
    expect(cat.human!.loop).toBeUndefined()
    expect(cat.human!.outputs_locked).toBe(true)
  })
  it('evaluates key rules like the Go side', () => {
    expect(holds([{ key: 'stdout_json', equals: 'true' }], { stdout_json: true })).toBe(true)
    expect(holds([{ key: 'stdout_json', equals: 'true' }], { stdout_json: 'true' })).toBe(true)
    expect(holds([{ key: 'stdout_json', equals: 'true' }], {})).toBe(false)
    expect(holds([{ input: 'task' }], {})).toBe(true)
  })
})

describe('field to operation', () => {
  it('a value is set as typed and undefined removes the key', () => {
    expect(configOp('a', 'command', '')).toEqual({ op: 'set_config', node: 'a', path: ['command'], value: '' })
    expect(configOp('a', 'args', undefined)).toEqual({ op: 'unset_config', node: 'a', path: ['args'] })
  })
  it('a loop field change is one targeted operation, never the whole block again', () => {
    expect(loopConfigOp('status', 'operator', 'ne')).toEqual({ op: 'set_loop_config', node: 'status', path: ['operator'], value: 'ne' })
    expect(loopConfigOp('status', 'expected', undefined)).toEqual({ op: 'unset_loop_config', node: 'status', path: ['expected'] })
    expect(loopAttemptsOp('status', 4)).toEqual({ op: 'set_loop_attempts', node: 'status', max_attempts: 4 })
    expect(loopAttemptsOp('status', undefined)).toEqual({ op: 'set_loop_attempts', node: 'status' })
  })
  it('an integer field takes only a plain decimal integer; anything else is a local parse error, never unset', () => {
    expect(parseInteger(' 7 ')).toBe(7)
    expect(parseInteger('0')).toBe(0)
    expect(parseInteger('-3')).toBe(-3)
    expect(parseInteger('')).toBe('')
    for (const bad of ['abc', '1.5', '0x10', 'Infinity', '1e3', '99999999999999999999']) expect(parseInteger(bad), bad).toBeNull()
  })
  it('a map key another row already has is caught before it can replace that entry', () => {
    const rows: [string, string][] = [['A', '1'], ['B', '2']]
    expect(keyTaken(rows, 'B', 0)).toBe(true)
    expect(keyTaken(rows, 'A', 0)).toBe(false)
    expect(keyTaken(rows, 'A', -1)).toBe(true)
    expect(keyTaken(rows, 'C', -1)).toBe(false)
  })
  it('parses typed Advanced text as JSON when it is JSON, else keeps the text', () => {
    expect(parseLoose('{"a": [1]}')).toEqual({ a: [1] })
    expect(parseLoose('{a: 1}')).toBe('{a: 1}')
    expect(parseLoose('7')).toBe(7)
  })
})

describe('section-level diagnostics', () => {
  it('a loop diagnostic no field shows (a gate input) is left for the section', () => {
    const at = (path: string) => ({ severity: 'error' as const, code: 'E', message: path, location: { path } })
    const field = at('nodes.s.loop.gate.config.operator')
    const input = at('nodes.s.loop.gate.inputs.doc')
    const gate = at('nodes.s.loop.gate')
    const rest = unshown([field, input, gate, at('nodes.other.loop')], 'nodes.s.loop', [[field], [gate]])
    expect(rest).toEqual([input])
  })
})

describe('loop starter', () => {
  it('binds the first output the json_predicate input accepts', () => {
    const n = def(shell).nodes.build!
    const s = loopStarter('build', { ...n, loop: undefined }, cat.shell!)
    expect(s).toMatchObject({ kind: 'op', op: { op: 'set_loop', max_attempts: 3, bindings: [{ from: 'candidate.outputs.json', to: 'gate.inputs.candidate' }] } })
  })
  it('asks for the candidate when no output fits, and for an output when there is none', () => {
    const n = def(loops).nodes.diffed!
    expect(loopStarter('diffed', { ...n, loop: undefined }, cat.shell!)).toEqual({ kind: 'pick', outputs: ['stdout'] })
    expect(loopStarter('x', { type: 'shell' }, cat.shell!)).toEqual({ kind: 'no-outputs' })
  })
})

describe('diagnostics by path', () => {
  const diags = (shell as unknown as OpenResponse).diagnostics
  it('matches the executor diagnostic to the Execution section, not to a field', () => {
    expect(diagnosticsExactly(diags, 'nodes.build.config')).toHaveLength(1)
    expect(diagnosticsAt(diags, 'nodes.build.config.command')).toHaveLength(0)
    expect(nodeDiagnostics(diags, 'build')).toHaveLength(1)
    expect(nodeDiagnostics(diags, 'lint')).toHaveLength(0)
  })
  it('does not match a prefix of another name', () => {
    const d = [{ severity: 'error', code: 'X', message: 'm', location: { path: 'nodes.ab.config' } }]
    expect(diagnosticsAt(d, 'nodes.a')).toHaveLength(0)
    expect(diagnosticsAt(d, 'nodes.ab')).toHaveLength(1)
  })
})

describe('input sources', () => {
  it('reports edges, graph inputs, literals and unbound inputs as declared', () => {
    expect(inputSources(def(shell), 'build', cat.shell)).toEqual({ target: [{ kind: 'graph', from: 'inputs.target' }] })
    expect(inputSources(def(shell), 'report', cat.shell)).toEqual({ text: [{ kind: 'edge', from: 'build.outputs.stdout' }] })
    expect(inputSources(def(types), 'agent', cat.agent)).toEqual({ task: [{ kind: 'graph', from: 'inputs.task' }] })
    const withLiteral: Definition = { ...def(types), nodes: { ...def(types).nodes, agent: { ...def(types).nodes.agent!, config: { task: 'do it' } } }, graph_input_bindings: [] }
    expect(inputSources(withLiteral, 'agent', cat.agent)).toEqual({ task: [{ kind: 'literal', key: 'task' }] })
    expect(inputSources(def(loops), 'diffed', cat.shell)).toEqual({ expected: [{ kind: 'unbound' }] })
  })
})

describe('port-edit preview', () => {
  it('lists what the engine removed with the port', () => {
    const after = def((removePort as { body: unknown }).body)
    const changes = describeChange(def(shell), after)
    expect(changes).toContainEqual({ what: 'edge', how: 'removed', text: 'build.outputs.stdout → report.inputs.text' })
    expect(changes).toContainEqual({ what: 'port', how: 'removed', text: 'build.outputs.stdout: string' })
    expect(changes.some((c) => c.what !== 'port')).toBe(true)
  })
  it('an unchanged definition has no changes', () => {
    expect(describeChange(def(shell), def(shell))).toEqual([])
  })
})
