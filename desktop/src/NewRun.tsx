import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { useI18n } from './i18n/context'
import { agentWorkspace, graphShape, headline, inputFields, preflightChecks, tally, type Check, type Tone } from './newRunModel'
import { canStart, preflightState } from './preflight'
import type { ChosenGraph, Diagnostic, RecentGraph } from './types'
import { Glyph, type GlyphKind } from './ui/glyphs'
import type { GraphSource } from './newRunSession'
import { SettledPick } from './settledPick'
import { useNewRun } from './useNewRun'
import type { Definition } from './editor/types'

// F2.5c: New run with preflight (Tarmind canvas, artboard "New run with preflight").
// Left: the graph, its declared inputs and the run options. Right: the preflight,
// which reacts to every choice on the left, and the one primary action.

const TONE_GLYPH: Record<Tone, GlyphKind> = { ok: 'ok', warn: 'warn', bad: 'bad', pending: 'running', idle: 'idle' }
const SEVERITY_GLYPH: Record<string, GlyphKind> = { error: 'bad', warning: 'warn' }

export function NewRun({ onStarted, initialGraph }: { onStarted: (runId: string) => void; initialGraph?: ChosenGraph }) {
  const { t } = useI18n()
  const s = useNewRun(initialGraph, onStarted)
  return (
    <div className="nr">
      <section className="nr-form" aria-label={t('newrun.setup')}>
        <h1>{t('config.title')}</h1>
        <GraphGroup graph={s.graph} source={s.source} recent={s.recent} def={s.def} locked={s.busy} pickError={s.pickError} onRecent={s.chooseRecent} onFile={s.chooseFile} />
        <InputsGroup chosen={!!s.graph} loading={s.validating} def={s.def} values={s.values} onChange={s.setValue} />
        <OptionsGroup def={s.def} />
      </section>
      <Preflight s={s} />
    </div>
  )
}

function Group({ title, lede, children }: { title: string; lede: string; children: ReactNode }) {
  return (
    <div className="nr-grp">
      <h2>{title}</h2>
      <p className="nr-lede">{lede}</p>
      {children}
    </div>
  )
}

const windowTimers = { set: (run: () => void, ms: number) => window.setTimeout(run, ms), clear: (h: unknown) => window.clearTimeout(h as number) }

/**
 * Picks from the recent list: a click or Space opens the graph at once; an arrow key
 * selects it and opens it only once the selection rests (SettledPick). Returns the
 * id shown as selected while it waits, and the handlers for each radio.
 */
function useRecentPicks(source: GraphSource | null, locked: boolean, onRecent: (r: RecentGraph) => void) {
  const [waiting, setWaiting] = useState<string | null>(null)
  const arrow = useRef(false)
  const open = useRef(onRecent)
  open.current = onRecent
  const [picker] = useState(() => new SettledPick<RecentGraph>((r) => open.current(r), (r) => setWaiting(r?.id ?? null), windowTimers))
  useEffect(() => () => picker.cancel(), [picker])
  // A run is being started: the graph cannot change, so a waiting selection is dropped.
  useEffect(() => {
    if (locked) picker.cancel()
  }, [locked, picker])
  const onKeyDown = (e: KeyboardEvent) => {
    arrow.current = e.key.startsWith('Arrow')
  }
  const pick = (r: RecentGraph | null) => {
    const byArrow = arrow.current
    arrow.current = false
    // Back on the graph already chosen (or the chosen file): nothing to open.
    if (!r || (source?.kind === 'recent' && source.id === r.id)) return picker.cancel()
    if (byArrow) picker.later(r)
    else picker.now(r)
  }
  return { waiting, onKeyDown, pick }
}

// While a run is being started the graph cannot change (`locked`).
function GraphGroup({ graph, source, recent, def, locked, pickError, onRecent, onFile }: { graph: ChosenGraph | null; source: GraphSource | null; recent: RecentGraph[]; def: Definition | null; locked: boolean; pickError: string | null; onRecent: (r: RecentGraph) => void; onFile: () => void }) {
  const { t } = useI18n()
  const { waiting, onKeyDown, pick } = useRecentPicks(source, locked, onRecent)
  const shape = def ? graphShape(def) : null
  const sub = shape ? t('newrun.graph.shape', { count: shape.nodes, types: shape.types.join(', ') }) : undefined
  const fileChosen = graph !== null && source?.kind === 'file'
  return (
    <Group title={t('newrun.graph')} lede={t('newrun.graph.lede')}>
      <div className="nr-opts" role="radiogroup" aria-label={t('newrun.graph')}>
        {fileChosen && <GraphOption name={graph.label} tag={t('newrun.graph.tag.file')} sub={sub} checked={waiting === null} disabled={locked} onKeyDown={onKeyDown} onPick={() => pick(null)} />}
        {recent.map((r) => {
          const on = source?.kind === 'recent' && source.id === r.id
          const checked = waiting === null ? on : waiting === r.id
          return <GraphOption key={r.id} name={on && graph ? graph.label : r.label} tag={t('newrun.graph.tag.recent')} sub={on ? sub : undefined} checked={checked} disabled={locked} onKeyDown={onKeyDown} onPick={() => pick(r)} />
        })}
      </div>
      {!graph && recent.length === 0 && <p className="nr-none mu">{t('newrun.graph.none')}</p>}
      {pickError && (
        <p className="nr-err" role="alert" data-testid="pick-error">
          {t('newrun.graph.pickFailed', { message: pickError })}
        </p>
      )}
      <div className="nr-choose">
        <button type="button" className="ib-btn" data-testid="choose" disabled={locked} onClick={onFile}>
          {t('newrun.graph.choose')}
        </button>
      </div>
    </Group>
  )
}

function GraphOption({ name, tag, sub, checked, disabled, onKeyDown, onPick }: { name: string; tag: string; sub?: string; checked: boolean; disabled: boolean; onKeyDown: (e: KeyboardEvent) => void; onPick: () => void }) {
  return (
    <label className="nr-opt">
      <input type="radio" name="nr-graph" checked={checked} disabled={disabled} onKeyDown={onKeyDown} onChange={onPick} />
      <span className="nr-opt-text">
        <span className="nr-opt-name" data-testid={checked ? 'graph-label' : undefined}>{name}</span>
        {sub && <span className="nr-opt-sub">{sub}</span>}
      </span>
      <span className="nr-opt-tag">{tag}</span>
    </label>
  )
}

function InputsGroup({ chosen, loading, def, values, onChange }: { chosen: boolean; loading: boolean; def: Definition | null; values: Readonly<Record<string, string>>; onChange: (name: string, value: string) => void }) {
  const { t } = useI18n()
  const fields = inputFields(def)
  const empty = !chosen ? 'newrun.inputs.noGraph' : loading ? 'newrun.inputs.loading' : !def ? 'newrun.inputs.unreadable' : fields.length === 0 ? 'newrun.inputs.none' : null
  return (
    <Group title={t('newrun.inputs')} lede={t('newrun.inputs.lede')}>
      {empty && <p className="nr-none mu">{t(empty)}</p>}
      {fields.map((f) => {
        const id = `nr-in-${f.name}`
        const hint = [t(f.required ? 'newrun.inputs.required' : 'newrun.inputs.optional'), f.type, ...(f.targets.length ? [t('newrun.inputs.passedTo', { nodes: f.targets.join(', ') })] : []), ...(!f.required && f.neededBy.length ? [t('newrun.inputs.neededBy', { nodes: f.neededBy.join(', ') })] : [])].join(' · ')
        return (
          <div className="nr-fld" key={f.name}>
            <label htmlFor={id}>{f.name}</label>
            <input id={id} type="text" value={values[f.name] ?? ''} onChange={(e) => onChange(f.name, e.target.value)} aria-describedby={`${id}-hint`} aria-required={f.required || undefined} data-testid="run-input" />
            <span className="nr-hint" id={`${id}-hint`}>{hint}</span>
          </div>
        )
      })}
    </Group>
  )
}

// The engine has no per-run setting for either option yet (issues linked from the F2.5c spec):
// both are shown as they will be, disabled, with where the value comes from today.
function OptionsGroup({ def }: { def: Definition | null }) {
  const { t } = useI18n()
  const { agents, isolated } = agentWorkspace(def)
  const worktree = agents > 0 && isolated === agents
  return (
    <Group title={t('newrun.options')} lede={t('newrun.options.lede')}>
      <div className="nr-sets">
        <div className="nr-set">
          <div className="nr-set-text">
            <b id="nr-conc">{t('newrun.options.concurrency')}</b>
            <span>{t('newrun.options.concurrencyDetail')}</span>
          </div>
          <div className="nr-step" role="group" aria-labelledby="nr-conc">
            <button type="button" aria-label={t('newrun.options.decrease')} disabled>
              −
            </button>
            <span className="mono">
              <span aria-hidden="true">—</span>
              <span className="sr-only">{t('newrun.options.engineValue')}</span>
            </span>
            <button type="button" aria-label={t('newrun.options.increase')} disabled>
              +
            </button>
          </div>
        </div>
        <div className="nr-set">
          <div className="nr-set-text">
            <b id="nr-ws">{t('newrun.options.workspace')}</b>
            <span>{t('newrun.options.workspaceDetail')}</span>
          </div>
          <div className="nr-seg" role="group" aria-labelledby="nr-ws">
            <button type="button" aria-pressed={!worktree} disabled>
              {t('newrun.options.shared')}
            </button>
            <button type="button" aria-pressed={worktree} disabled>
              {t('newrun.options.worktree')}
            </button>
          </div>
        </div>
      </div>
    </Group>
  )
}

function CheckRow({ tone, title, detail, mono, testId }: { tone: Tone; title: ReactNode; detail?: ReactNode; mono?: boolean; testId?: string }) {
  return (
    <li className="nr-chk" data-tone={tone} data-testid={testId}>
      <span className="nr-chk-glyph">
        <Glyph kind={TONE_GLYPH[tone]} />
      </span>
      <div>
        <b>{title}</b>
        {detail !== undefined && detail !== '' && <span className={mono ? 'nr-chk-d mono' : 'nr-chk-d'}>{detail}</span>}
      </div>
    </li>
  )
}

function DiagnosticRow({ d }: { d: Diagnostic }) {
  const { t } = useI18n()
  const word = d.severity === 'error' || d.severity === 'warning' ? t(`config.severity.${d.severity}`) : t('config.severity.info')
  return (
    <li className="nr-chk nr-diag" data-testid="diagnostic">
      <span className="nr-chk-glyph">
        <Glyph kind={SEVERITY_GLYPH[d.severity] ?? 'idle'} />
      </span>
      <div>
        <b>
          <span className="sr-only">{word}: </span>
          <span className="mono">{d.code}</span>
          {d.location?.node_id && <span className="tag">{d.location.node_id}</span>}
        </b>
        <span className="nr-chk-d">{d.message}</span>
        {d.suggestion && <span className="nr-chk-d">{d.suggestion}</span>}
      </div>
    </li>
  )
}

function checkDetail(t: ReturnType<typeof useI18n>['t'], c: Check): ReactNode {
  if (!c.detail) return undefined
  return 'raw' in c.detail ? c.detail.raw : t(c.detail.key, c.detail.params)
}

function Preflight({ s }: { s: ReturnType<typeof useNewRun> }) {
  const { t } = useI18n()
  const facts = { chosen: !!s.graph, validating: s.validating, validation: s.validation, validationError: s.validationError, def: s.def, capabilities: s.capabilities, values: s.values }
  const checks = preflightChecks(facts)
  const diagnostics = s.validation?.diagnostics ?? []
  const counts = tally(checks, diagnostics)
  const state = preflightState(s.validation)
  const head = headline(state, facts, counts.warnings, diagnostics.filter((d) => d.severity === 'error').length)
  const summary = [t('newrun.summary.passed', { count: counts.passed }), ...(counts.warnings ? [t('newrun.summary.warnings', { count: counts.warnings })] : []), ...(counts.failed ? [t('newrun.summary.failed', { count: counts.failed })] : [])].join(', ')
  const graphCheck = checks[0]!
  return (
    <aside className="nr-pre" aria-label={t('newrun.preflight')} data-testid="preflight-card">
      <div>
        <h2 className="nr-label">{t('newrun.preflight')}</h2>
        <p className="nr-head" data-testid="preflight-status" data-state={s.graph ? state : 'none'} role="status">
          {t(head.key, head.params)}
        </p>
      </div>
      <ul className="nr-chks">
        <CheckRow tone={graphCheck.tone} title={t(graphCheck.title)} detail={checkDetail(t, graphCheck)} testId={s.validating ? 'validating' : 'check-graph'} />
        {diagnostics.map((d, i) => (
          <DiagnosticRow key={i} d={d} />
        ))}
        {checks.slice(1).map((c) => (
          <CheckRow key={c.id} tone={c.tone} title={t(c.title, c.titleParams)} detail={checkDetail(t, c)} mono={!!c.detail && 'raw' in c.detail && c.detail.mono} testId={`check-${c.id}`} />
        ))}
        {s.startError && <CheckRow tone="bad" title={t('newrun.check.start.failed')} detail={s.startError.message} testId="start-error" />}
        {s.startError?.diagnostics.map((d, i) => (
          <DiagnosticRow key={`start-${i}`} d={d} />
        ))}
      </ul>
      <div className="nr-go">
        <span className="nr-sum mu">{s.graph && !s.validating ? summary : ''}</span>
        <button type="button" className="ib-btn pr" data-testid="run" disabled={!s.graph || !canStart(state) || s.busy} onClick={() => void s.run()}>
          {t('newrun.run')}
        </button>
      </div>
    </aside>
  )
}
