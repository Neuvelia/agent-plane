import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import capabilities from './fixtures/capabilities-linux.json'
import configErrorValidate from './fixtures/graph-validate-config-error.json'
import fixIssueOpen from './fixtures/graph-open-fix-issue.json'
import fixIssueValidate from './fixtures/graph-validate-fix-issue.json'
import demoOpen from './fixtures/graph-open-demo.json'
import type { CapabilityReport, CapabilityResult } from '../src/capabilities'
import type { Definition } from '../src/editor/types'
import { I18nProvider } from '../src/i18n/context'
import { agentWorkspace, graphShape, headline, inputFields, inputLines, preflightChecks, tally, type PreflightFacts } from '../src/newRunModel'
import { NewRun } from '../src/NewRun'
import { preflightState } from '../src/preflight'

const render = (initialGraph?: { grant: string; label: string }) =>
  renderToStaticMarkup(
    <I18nProvider locale="en">
      <NewRun onStarted={() => undefined} initialGraph={initialGraph} />
    </I18nProvider>,
  )

// Recorded answers of the real engine (scripts/record-graph-fixtures.cjs).
const fixIssue = fixIssueOpen.definition as unknown as Definition
const demo = demoOpen.definition as unknown as Definition
const linux = capabilities as unknown as CapabilityReport
const withCodex: CapabilityResult = { kind: 'ok', report: { ...linux, tools: linux.tools.map((t) => (t.id === 'codex' ? { ...t, status: 'available', version: '0.169.0', reason: '' } : t)) } }
const isolated = (def: Definition): Definition => ({ ...def, nodes: Object.fromEntries(Object.entries(def.nodes).map(([id, n]) => [id, n.type === 'agent' ? { ...n, config: { ...n.config, isolation: 'worktree' } } : n])) })

const facts = (over: Partial<PreflightFacts>): PreflightFacts => ({ chosen: true, validating: false, validation: fixIssueValidate, validationError: null, def: fixIssue, capabilities: withCodex, values: { issue: '#142 Range parser drops an open upper bound' }, ...over })
const tones = (f: PreflightFacts) => preflightChecks(f).map((c) => `${c.id}:${c.tone}`)

describe('New run holds a grant and a file name, never a path', () => {
  it('has no field to type or show a filesystem path', () => {
    const out = render()
    expect(out).not.toContain('data-testid="graph-path"')
    expect(out).not.toContain('/path/to/graph.yaml')
    expect(out).toContain('data-testid="choose"')
  })

  it('shows the file name of a graph chosen on the Inbox, selected, and not its grant', () => {
    const out = render({ grant: 'grant-7', label: 'demo-vertical.yaml' })
    expect(out).toMatch(/<input type="radio"[^>]*checked=""[^>]*\/>.*data-testid="graph-label">demo-vertical.yaml</)
    expect(out).not.toContain('grant-7')
  })

  it('cannot start before a graph is chosen, and says what to do', () => {
    const out = render()
    expect(out).toMatch(/<button[^>]*data-testid="run"[^>]*disabled/)
    expect(out).toContain('Choose a graph')
  })

  it('shows the per-run options disabled until the engine accepts them', () => {
    const out = render()
    expect(out).toMatch(/aria-label="Decrease" disabled/)
    expect(out).toMatch(/aria-pressed="true" disabled[^>]*>Shared/)
  })
})

describe('New run model over recorded graphs', () => {
  it('lists declared inputs with their type and the nodes they are bound to', () => {
    expect(inputFields(fixIssue)).toEqual([{ name: 'issue', type: 'string', required: true, targets: ['confirm_start', 'fix'] }])
    expect(inputFields(demo)).toEqual([])
    expect(inputFields(null)).toEqual([])
  })

  it('starts with name=value lines and leaves empty fields out', () => {
    const fields = inputFields(fixIssue)
    expect(inputLines(fields, { issue: 'a=b c' })).toEqual(['issue=a=b c'])
    expect(inputLines(fields, { issue: '' })).toEqual([])
  })

  it('summarises nodes and kinds as received, and reads the Agent isolation setting', () => {
    expect(graphShape(fixIssue)).toEqual({ nodes: 4, types: ['human', 'agent', 'shell'] })
    expect(agentWorkspace(fixIssue)).toEqual({ agents: 1, isolated: 0 })
    expect(agentWorkspace(isolated(fixIssue))).toEqual({ agents: 1, isolated: 1 })
    expect(agentWorkspace(demo)).toEqual({ agents: 0, isolated: 0 })
  })
})

describe('preflight rows and headline', () => {
  it('the artboard: valid graph, Codex found, input filled, shared working tree', () => {
    const f = facts({})
    expect(tones(f)).toEqual(['graph:ok', 'agent:ok', 'inputs:ok', 'workspace:warn'])
    const counts = tally(preflightChecks(f), [])
    expect(counts).toEqual({ passed: 4, warnings: 1, failed: 0 })
    expect(headline(preflightState(f.validation), f, counts.warnings, 0)).toEqual({ key: 'newrun.head.warnings', params: { count: 1 } })
  })

  it('every Agent node in its own worktree: ready to run', () => {
    const f = facts({ def: isolated(fixIssue) })
    expect(tones(f)).toEqual(['graph:ok', 'agent:ok', 'inputs:ok', 'workspace:ok'])
    expect(headline(preflightState(f.validation), f, tally(preflightChecks(f), []).warnings, 0)).toEqual({ key: 'newrun.head.ready' })
  })

  it('a missing required input and an unconfigured Codex warn; they do not block', () => {
    const f = facts({ values: {}, capabilities: { kind: 'ok', report: linux } })
    expect(tones(f)).toEqual(['graph:ok', 'agent:warn', 'inputs:warn', 'workspace:warn'])
    expect(preflightChecks(f).find((c) => c.id === 'agent')?.detail).toEqual({ key: 'cap.meaning.codex.notConfigured' })
    expect(preflightState(f.validation)).toBe('ready')
  })

  it('asks for Codex only for a graph with an Agent node, and shows it pending until answered', () => {
    expect(tones(facts({ def: demo, validation: { status: 'valid', diagnostics: [] } }))).toEqual(['graph:ok'])
    expect(tones(facts({ capabilities: 'loading' }))[1]).toBe('agent:pending')
    expect(tones(facts({ capabilities: { kind: 'unreachable' } }))[1]).toBe('agent:warn')
  })

  it('an engine error blocks, and the headline counts the errors', () => {
    const f = facts({ def: null, validation: configErrorValidate })
    expect(tones(f)).toEqual(['graph:bad'])
    expect(tally(preflightChecks(f), f.validation!.diagnostics)).toEqual({ passed: 0, warnings: 0, failed: 1 })
    expect(headline(preflightState(f.validation), f, 0, 1)).toEqual({ key: 'newrun.head.blockedBy', params: { count: 1 } })
  })

  it('no graph, checking, and a check that could not reach the engine', () => {
    expect(tones(facts({ chosen: false }))).toEqual(['graph:idle'])
    expect(headline('idle', { chosen: false, validating: false, validation: null }, 0, 0).key).toBe('newrun.head.choose')
    expect(tones(facts({ validating: true, validation: null }))[0]).toBe('graph:pending')
    const lost = facts({ validation: null, validationError: 'the agentplane sidecar is not running', def: null })
    expect(preflightChecks(lost)[0]).toMatchObject({ tone: 'bad', detail: { raw: 'the agentplane sidecar is not running' } })
    expect(headline('idle', lost, 0, 0).key).toBe('newrun.head.unchecked')
  })
})
