import { describe, expect, it } from 'vitest'
import capabilities from './fixtures/capabilities-linux.json'
import fixIssueOpen from './fixtures/graph-open-fix-issue.json'
import fixIssueValidate from './fixtures/graph-validate-fix-issue.json'
import type { ApiFailure } from '../src/api'
import type { CapabilityReport, CapabilityResult } from '../src/capabilities'
import type { Definition } from '../src/editor/types'
import { preflightChecks } from '../src/newRunModel'
import { NewRunSession, type NewRunPorts } from '../src/newRunSession'
import { attachSession } from '../src/useNewRun'
import type { Diagnostic, GraphGrantResult } from '../src/types'

// F2.5c requirement 2 and the failure behavior: ordering and grant rules of New run,
// over a hand-written fake of the bridge whose answers the test releases one by one.

type Validation = { status: string; diagnostics: Diagnostic[] } | ApiFailure

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

const flush = () => new Promise<void>((r) => setTimeout(r, 0))
const fixIssue = fixIssueOpen.definition as unknown as Definition
const valid: Validation = { status: 'valid', diagnostics: [] }

class FakeBridge implements NewRunPorts {
  closed: string[] = []
  started: { grant: string; inputs: string[] }[] = []
  recentReads = 0
  picks: ReturnType<typeof deferred<GraphGrantResult>>[] = []
  validations = new Map<string, ReturnType<typeof deferred<Validation>>>()
  startAnswer: { runId: string } | ApiFailure = { runId: 'run-1' }
  capabilityAnswer: Promise<CapabilityResult> = Promise.resolve({ kind: 'ok', report: capabilities as unknown as CapabilityReport })

  openGraph = () => this.nextPick()
  openRecentGraph = () => this.nextPick()
  recentGraphs = async () => {
    this.recentReads += 1
    return [{ id: 'r1', label: 'a.yaml' }]
  }
  closeGraph = async (grant: string) => {
    this.closed.push(grant)
    return true
  }
  validate = (grant: string) => this.validation(grant).promise
  definition = async () => fixIssue
  start = async (grant: string, inputs: string[]) => {
    this.started.push({ grant, inputs })
    return this.startAnswer
  }
  capabilities = () => this.capabilityAnswer

  validation(grant: string) {
    if (!this.validations.has(grant)) this.validations.set(grant, deferred<Validation>())
    return this.validations.get(grant)!
  }
  private nextPick() {
    const d = deferred<GraphGrantResult>()
    this.picks.push(d)
    return d.promise
  }
}

const granted = (grant: string): GraphGrantResult => ({ ok: true, grant, label: `${grant}.yaml`, exists: true })

async function chosen(bridge: FakeBridge, session: NewRunSession, grant: string) {
  const done = session.chooseFile()
  bridge.picks.at(-1)!.resolve(granted(grant))
  await done
}

describe('New run session: picks and grants', () => {
  it('drops a late answer for a graph that is no longer chosen and releases its grant', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    await chosen(bridge, session, 'A')
    await chosen(bridge, session, 'B')
    expect(bridge.closed).toEqual(['A'])
    bridge.validation('B').resolve(valid)
    await flush()
    bridge.validation('A').resolve({ status: 'invalid', diagnostics: [{ severity: 'error', code: 'X', message: 'from A' }] })
    await flush()
    expect(session.snapshot()).toMatchObject({ graph: { grant: 'B' }, validation: valid, validating: false })
  })

  it('only the newest pick counts; the grant of an older pick is released', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    const first = session.chooseFile()
    const second = session.chooseFile()
    bridge.picks[1]!.resolve(granted('B'))
    await second
    bridge.picks[0]!.resolve(granted('A'))
    await first
    expect(session.snapshot().graph?.grant).toBe('B')
    expect(bridge.closed).toEqual(['A'])
  })

  it('a grant that arrives after the screen was left is released, and the chosen one is released on leaving', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    await chosen(bridge, session, 'A')
    const late = session.chooseFile()
    session.dispose()
    bridge.picks.at(-1)!.resolve(granted('B'))
    await late
    expect(bridge.closed.sort()).toEqual(['A', 'B'])
  })

  it('a failed pick is not a refused start, and the recent list is read again', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    session.start()
    await flush()
    const done = session.chooseRecent({ id: 'r1', label: 'a.yaml' })
    bridge.picks.at(-1)!.resolve({ ok: false, code: 'not_found', message: 'that graph is no longer in the list' })
    await done
    await flush()
    expect(session.snapshot()).toMatchObject({ pickError: 'that graph is no longer in the list', startError: null, graph: null })
    expect(bridge.recentReads).toBe(2)
  })

  it('a dialog that throws shows a pick error instead of an unhandled rejection', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    const done = session.chooseFile()
    bridge.picks.at(-1)!.reject(new Error('IPC failed'))
    await done
    expect(session.snapshot().pickError).toBe('IPC failed')
  })
})

describe('New run session: starting', () => {
  it('starts with the filled inputs and cannot change graph while starting', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    await chosen(bridge, session, 'A')
    bridge.validation('A').resolve(valid)
    await flush()
    session.setValue('issue', '#142')
    let runId = ''
    const running = session.run((id) => (runId = id))
    expect(session.snapshot().busy).toBe(true)
    await session.chooseFile() // ignored while starting
    expect(bridge.picks).toHaveLength(1)
    await running
    expect(bridge.started).toEqual([{ grant: 'A', inputs: ['issue=#142'] }])
    expect(runId).toBe('run-1')
  })

  it('a refused start shows the engine message and diagnostics', async () => {
    const bridge = new FakeBridge()
    bridge.startAnswer = { message: 'the graph file is not valid', diagnostics: [{ severity: 'error', code: 'E', message: 'bad' }] }
    const session = new NewRunSession(bridge)
    await chosen(bridge, session, 'A')
    bridge.validation('A').resolve(valid)
    await flush()
    await session.run(() => undefined)
    expect(session.snapshot()).toMatchObject({ busy: false, startError: bridge.startAnswer })
  })
})

describe('New run session: checks', () => {
  it('an unavailable capability report makes the Codex row warn', async () => {
    const bridge = new FakeBridge()
    const report = deferred<CapabilityResult>()
    bridge.capabilityAnswer = report.promise
    const session = new NewRunSession(bridge)
    await chosen(bridge, session, 'A')
    bridge.validation('A').resolve(fixIssueValidate)
    await flush()
    expect(session.snapshot().capabilities).toBe('loading')
    report.resolve({ kind: 'failed', status: 500 })
    await report.promise
    await flush()
    const s = session.snapshot()
    const agent = preflightChecks({ chosen: true, validating: false, validation: s.validation, validationError: null, def: s.def, capabilities: s.capabilities, values: {} }).find((c) => c.id === 'agent')
    expect(agent).toMatchObject({ tone: 'warn', title: 'newrun.check.codex.unknown' })
  })

  it('checking again keeps the typed values and the shown result until the answer arrives', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    await chosen(bridge, session, 'A')
    bridge.validation('A').resolve(valid)
    await flush()
    session.setValue('issue', '#142')
    bridge.validations.delete('A')
    session.recheck()
    expect(session.snapshot()).toMatchObject({ validation: valid, validating: false })
    bridge.validation('A').resolve({ status: 'invalid', diagnostics: [{ severity: 'error', code: 'E', message: 'saved with an error' }] })
    await flush()
    expect(session.snapshot()).toMatchObject({ values: { issue: '#142' }, validation: { status: 'invalid' } })
  })

  it('an engine that cannot be reached leaves the graph unchecked with the reason', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    await chosen(bridge, session, 'A')
    bridge.validation('A').reject(new Error('the agentplane sidecar is not running'))
    await flush()
    expect(session.snapshot()).toMatchObject({ validation: null, validationError: 'the agentplane sidecar is not running', validating: false })
  })
})

describe('New run session: review findings', () => {
  it('a pick that resolves while a run is starting keeps the graph and releases the new grant', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    await chosen(bridge, session, 'A')
    bridge.validation('A').resolve(valid)
    await flush()
    session.setValue('issue', '#142')
    const pick = session.chooseRecent({ id: 'r1', label: 'a.yaml' }) // the dialog is still open
    const start = deferred<{ runId: string }>()
    bridge.start = async (grant, inputs) => {
      bridge.started.push({ grant, inputs })
      return start.promise
    }
    const running = session.run(() => undefined)
    bridge.picks.at(-1)!.resolve(granted('B'))
    await pick
    expect(session.snapshot()).toMatchObject({ graph: { grant: 'A' }, values: { issue: '#142' }, busy: true })
    expect(bridge.closed).toEqual(['B'])
    start.resolve({ runId: 'run-1' })
    await running
    expect(bridge.started).toEqual([{ grant: 'A', inputs: ['issue=#142'] }])
  })

  it('an older check of the same graph that answers last does not replace a newer one', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge)
    await chosen(bridge, session, 'A')
    bridge.validation('A').resolve(valid)
    await flush()
    const older = deferred<Validation>()
    const newer = deferred<Validation>()
    const answers = [older, newer]
    bridge.validate = () => answers.shift()!.promise
    session.recheck()
    session.recheck()
    newer.resolve(valid)
    await flush()
    older.resolve({ status: 'invalid', diagnostics: [{ severity: 'error', code: 'E', message: 'old file' }] })
    await flush()
    expect(session.snapshot().validation).toEqual(valid)
  })
})

describe('New run lifetime (attachSession)', () => {
  class Target {
    listeners = new Set<() => void>()
    addEventListener = (_: string, l: () => void) => void this.listeners.add(l)
    removeEventListener = (_: string, l: () => void) => void this.listeners.delete(l)
  }
  const microtask = () => new Promise<void>((r) => queueMicrotask(r))

  it('a StrictMode remount keeps the session; a real unmount releases the chosen grant', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge, { grant: 'A', label: 'a.yaml' })
    const mounts = { current: 0 }
    const target = new Target()
    attachSession(session, target, mounts)() // StrictMode: mount, unmount...
    const cleanup = attachSession(session, target, mounts) // ...and mount again at once
    await microtask()
    expect(bridge.closed).toEqual([])
    cleanup()
    await microtask()
    expect(bridge.closed).toEqual(['A'])
    expect(target.listeners.size).toBe(0)
  })

  it('coming back to the window checks the chosen graph again', async () => {
    const bridge = new FakeBridge()
    const session = new NewRunSession(bridge, { grant: 'A', label: 'a.yaml' })
    const target = new Target()
    attachSession(session, target, { current: 0 })
    bridge.validation('A').resolve(valid)
    await flush()
    let checks = 0
    bridge.validate = async () => {
      checks += 1
      return valid
    }
    for (const l of target.listeners) l()
    await flush()
    expect(checks).toBe(1)
  })
})

describe('New run session: first frame', () => {
  it('a graph passed in (Inbox "Open graph") is shown as being checked before the check starts', () => {
    const s = new NewRunSession(new FakeBridge(), { grant: 'A', label: 'a.yaml' }).snapshot()
    expect(s).toMatchObject({ validating: true, validation: null, validationError: null })
    expect(preflightChecks({ chosen: true, validating: s.validating, validation: s.validation, validationError: s.validationError, def: s.def, capabilities: s.capabilities, values: s.values })[0]).toMatchObject({ tone: 'pending' })
  })
})
