import { useEffect, useId, useMemo, useRef, useState, type AnimationEvent, type KeyboardEvent, type MouseEvent, type RefObject } from 'react'
import { listRuns } from '../api'
import { useI18n } from '../i18n/context'
import type { MessageKey } from '../i18n'
import { activeIndex, createCloser, cycleScope, filterEntries, moveActive, outside, paletteKey, parseQuery, readPaletteRuns, shortcutText, type PaletteEntry, type PaletteGroup, type PaletteKeyAction, type PaletteRun, type RunsState } from '../palette'
import { Icon, type IconName } from './icons'

// F2.5b command palette (design board "Command palette"). A modal <dialog>:
// focus stays in the search field, the results are a listbox driven through
// aria-activedescendant, Escape, a click outside or the palette shortcut close
// it, and closing returns focus to where it was. Entries only navigate or open
// what the shell already offers; the palette decides nothing about runs.
// Key and close decisions live in palette.ts (paletteKey, createCloser) and are unit-tested.

export type { RunsState }

/** An entry the palette can perform. */
export interface PaletteCommand extends PaletteEntry {
  icon: IconName
  perform: () => void
}

/** Longer than the 120 ms exit animation in styles.css (.cmdk.closing). */
const EXIT_FALLBACK_MS = 300

const GROUP_KEY: Record<PaletteGroup, MessageKey> = { actions: 'palette.group.actions', runs: 'palette.group.runs', goto: 'palette.group.goto' }

/** True when the platform's command key is Cmd (macOS). */
export function isMac(): boolean {
  return typeof navigator !== 'undefined' && /Mac/i.test(navigator.userAgent)
}

/**
 * Reads the run list each time a palette opens. `session` identifies the open palette (null
 * when closed); an answer belongs to the session that asked, so runs from an earlier opening are
 * never shown (their status may be stale). A failed read is an error, never "no runs".
 */
export function usePaletteRuns(session: number | null, read: () => Promise<PaletteRun[]> = listRuns): { status: RunsState; runs: PaletteRun[] } {
  const [state, setState] = useState<{ session: number | null; status: RunsState; runs: PaletteRun[] }>({ session: null, ...NOT_READ })
  useEffect(() => {
    if (session === null) return
    let alive = true
    void readPaletteRuns(read).then((r) => alive && setState({ session, ...r }))
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session])
  return state.session === session && session !== null ? { status: state.status, runs: state.runs } : NOT_READ
}

const NOT_READ: { status: RunsState; runs: PaletteRun[] } = { status: 'loading', runs: [] }

export interface CommandPaletteProps {
  commands: PaletteCommand[]
  runs: RunsState
  mac: boolean
  /** Called once the palette has finished closing (after its exit animation); reopen asks for a fresh palette. */
  onClose: (reopen: boolean) => void
  /** Initial query, for stories and tests. */
  initialQuery?: string
}

export function CommandPalette({ commands, runs, mac, onClose, initialQuery = '' }: CommandPaletteProps) {
  const { t } = useI18n()
  const ref = useRef<HTMLDialogElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const [query, setQuery] = useState(initialQuery)
  // The active entry by id: runs that arrive while the palette is open do not move it.
  const [activeId, setActiveId] = useState<string | null>(null)
  const [closing, setClosing] = useState(false)
  const closer = useRef(createCloser()).current
  const pressedBackdrop = useRef(false)
  const listRef = useRef<HTMLDivElement>(null)
  const shown = useMemo(() => filterEntries(commands, query, (g) => t(GROUP_KEY[g])), [commands, query, t])
  const current = activeIndex(shown, activeId)
  const base = useId()
  const optionId = (i: number) => `${base}-opt-${i}`

  useEffect(() => {
    const d = ref.current
    if (d && !d.open && typeof d.showModal === 'function') d.showModal()
    return () => d?.close()
  }, [])
  // Keep the active row in view when it moves and after every edit of the query (the list may have
  // been scrolled away with the wheel). Not on other re-renders, so a wheel scroll is not undone.
  useEffect(() => {
    if (current >= 0) document.getElementById(optionId(current))?.scrollIntoView?.({ block: 'nearest' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current, query])

  const finish = () => closer.finish({ close: () => ref.current?.close(), onClose })
  const dismiss = (next?: () => void) => {
    const d = ref.current
    // With reduced motion (or no layout) there is no exit animation to wait for.
    const animated = !!d && typeof getComputedStyle === 'function' && getComputedStyle(d).animationName !== 'none'
    const step = closer.dismiss(next, animated)
    if (step === 'finish') finish()
    if (step === 'animate') setClosing(true)
  }
  // A hidden window may never deliver animationend: close anyway shortly after the exit should have ended.
  useEffect(() => {
    if (!closing) return
    const timer = setTimeout(finish, EXIT_FALLBACK_MS)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [closing])
  const onAnimationEnd = (e: AnimationEvent<HTMLDialogElement>) => {
    if (closing && e.target === ref.current && e.animationName === 'cmdk-out') finish()
  }
  const choose = (i: number) => {
    const c = shown[i]
    if (c) dismiss(c.perform)
  }
  const edit = (q: string) => {
    setQuery(q)
    setActiveId(null)
  }
  const act: Record<PaletteKeyAction, () => void> = {
    // From the latest state: presses that arrive before a re-render each still move one row.
    next: () => setActiveId((id) => shown[moveActive(activeIndex(shown, id), shown.length, 'ArrowDown')]?.id ?? null),
    previous: () => setActiveId((id) => shown[moveActive(activeIndex(shown, id), shown.length, 'ArrowUp')]?.id ?? null),
    choose: () => choose(current),
    scopeNext: () => edit(cycleScope(query, 1)),
    scopePrevious: () => edit(cycleScope(query, -1)),
    // While the exit plays, the chord asks for the palette again once this one has closed.
    close: () => {
      if (!closer.requestReopen()) dismiss()
    },
  }
  // Keys are handled for the whole dialog, so they work wherever focus sits inside it.
  const onKeyDown = (e: KeyboardEvent<HTMLDialogElement>) => {
    const action = paletteKey({ key: e.key, ctrlKey: e.ctrlKey, metaKey: e.metaKey, shiftKey: e.shiftKey, altKey: e.altKey, repeat: e.repeat, composing: e.nativeEvent.isComposing }, mac)
    if (!action) return
    e.preventDefault()
    act[action]()
  }
  // The backdrop is judged by pointer position (see outside). A press anywhere but the field keeps
  // focus in the field; on the listbox itself (its scrollbar) the browser keeps the press, and the
  // listbox hands focus back to the field.
  const onBackdrop = (e: MouseEvent<HTMLDialogElement>) => !!ref.current && outside(ref.current.getBoundingClientRect(), e.clientX, e.clientY)
  const onMouseDown = (e: MouseEvent<HTMLDialogElement>) => {
    pressedBackdrop.current = onBackdrop(e)
    if (e.target !== inputRef.current && e.target !== listRef.current) e.preventDefault()
  }

  const listId = `${base}-list`
  const searching = parseQuery(query).scope !== 'actions'
  return (
    <dialog
      ref={ref}
      className={closing ? 'cmdk closing' : 'cmdk'}
      aria-label={t('palette.title')}
      data-testid="command-palette"
      onCancel={(e) => {
        e.preventDefault()
        dismiss()
      }}
      onKeyDown={onKeyDown}
      onMouseDown={onMouseDown}
      onClick={(e) => pressedBackdrop.current && onBackdrop(e) && dismiss()}
      onAnimationEnd={onAnimationEnd}
    >
      <div className="cmdk-q">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true" focusable="false">
          <circle cx="11" cy="11" r="6.5" />
          <path d="m20 20-4-4" />
        </svg>
        <input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-label={t('palette.search')}
          aria-expanded={shown.length > 0}
          aria-controls={shown.length > 0 ? listId : undefined}
          aria-activedescendant={current >= 0 ? optionId(current) : undefined}
          aria-autocomplete="list"
          autoComplete="off"
          spellCheck={false}
          placeholder={t('palette.placeholder')}
          value={query}
          onChange={(e) => edit(e.target.value)}
          data-testid="palette-input"
        />
        <kbd>esc</kbd>
      </div>
      <div className="cmdk-res">
        {shown.length === 0 ? (
          <div className="cmdk-none" data-testid="palette-none">
            {t('palette.none', { q: query.trim() })}
          </div>
        ) : (
          <Results listRef={listRef} onListFocus={() => inputRef.current?.focus()} id={listId} shown={shown} current={current} optionId={optionId} mac={mac} onHover={(i) => setActiveId(shown[i]?.id ?? null)} onChoose={choose} />
        )}
        {searching && runs !== 'ready' && (
          <div className={runs === 'error' ? 'cmdk-note bad' : 'cmdk-note'} data-testid="palette-runs-state">
            {t(runs === 'error' ? 'palette.runsError' : 'palette.runsLoading')}
          </div>
        )}
      </div>
      {/* One live region that stays mounted: screen readers announce changes to it, not regions inserted with text. */}
      <div className="sr-only" role="status" data-testid="palette-status">
        {announcement({ t, query, count: shown.length, runs, searching })}
      </div>
      <Footer />
    </dialog>
  )
}

/** What the live region says: no match, the run list's state, or the scope and number of results. */
function announcement({ t, query, count, runs, searching }: { t: (k: MessageKey, p?: Record<string, string | number>) => string; query: string; count: number; runs: RunsState; searching: boolean }): string {
  if (count === 0) return t('palette.none', { q: query.trim() })
  if (searching && runs === 'error') return t('palette.runsError')
  if (searching && runs === 'loading') return t('palette.runsLoading')
  const scope = parseQuery(query).scope
  const results = t('palette.resultsCount', { count })
  return scope === 'all' ? results : `${t(scope === 'actions' ? 'palette.scope.actions' : 'palette.scope.runs')} ${results}`
}

interface ResultsProps {
  listRef: RefObject<HTMLDivElement | null>
  onListFocus: () => void
  id: string
  shown: PaletteCommand[]
  current: number
  optionId: (i: number) => string
  mac: boolean
  onHover: (i: number) => void
  onChoose: (i: number) => void
}

function Results({ listRef, onListFocus, id, shown, current, optionId, mac, onHover, onChoose }: ResultsProps) {
  const { t } = useI18n()
  const groups = groupRuns(shown)
  return (
    // The listbox scrolls; it is focusable so its scroll region is reachable (axe), and gives focus straight back to the field.
    <div ref={listRef} className="cmdk-list" role="listbox" id={id} aria-label={t('palette.results')} tabIndex={0} onFocus={onListFocus}>
      {groups.map(({ group, start, items }) => (
        <div key={group} role="group" aria-labelledby={`${id}-${group}`}>
          <div className="cmdk-g" id={`${id}-${group}`} role="presentation">
            {t(GROUP_KEY[group])}
          </div>
          {items.map((c, j) => {
            const i = start + j
            return (
              <div
                key={c.id}
                id={optionId(i)}
                role="option"
                aria-selected={i === current}
                className={i === current ? 'cmdk-it on' : 'cmdk-it'}
                onMouseMove={() => i !== current && onHover(i)}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => onChoose(i)}
                data-testid={`palette-item-${c.id}`}
              >
                <span className="cmdk-ic" aria-hidden="true">
                  <Icon name={c.icon} size={13} />
                </span>
                <span className="cmdk-label">{c.label}</span>
                <span className="cmdk-hint">
                  {c.hint}
                  {c.shortcut && <kbd>{shortcutText(c.shortcut, mac)}</kbd>}
                </span>
              </div>
            )
          })}
        </div>
      ))}
    </div>
  )
}

/** Splits the (already grouped) entries into runs of one group, keeping each entry's overall index. */
function groupRuns(shown: PaletteCommand[]): { group: PaletteGroup; start: number; items: PaletteCommand[] }[] {
  const out: { group: PaletteGroup; start: number; items: PaletteCommand[] }[] = []
  shown.forEach((c, i) => {
    const last = out[out.length - 1]
    if (last && last.group === c.group) last.items.push(c)
    else out.push({ group: c.group, start: i, items: [c] })
  })
  return out
}

function Footer() {
  const { t } = useI18n()
  return (
    <div className="cmdk-foot">
      <span>
        <kbd>↑</kbd>
        <kbd>↓</kbd> {t('palette.navigate')}
      </span>
      <span>
        <kbd>↵</kbd> {t('palette.open')}
      </span>
      <span>
        <kbd>⇥</kbd> {t('palette.filter')}
      </span>
      <span className="cmdk-prefixes">
        {t('palette.type')} <span className="mono">&gt;</span> {t('palette.forActions')}, <span className="mono">#</span> {t('palette.forRuns')}
      </span>
    </div>
  )
}
