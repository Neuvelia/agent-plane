import { useEffect, useId, useMemo, useRef, useState, type AnimationEvent, type KeyboardEvent, type MouseEvent } from 'react'
import { listRuns } from '../api'
import { useI18n } from '../i18n/context'
import type { MessageKey } from '../i18n'
import { activeIndex, createCloser, cycleScope, filterEntries, moveActive, paletteKey, parseQuery, readPaletteRuns, shortcutText, type PaletteEntry, type PaletteGroup, type PaletteKeyAction, type PaletteRun, type RunsState } from '../palette'
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
 * Reads the run list each time the palette opens. Runs from an earlier opening are not
 * shown while it loads (their status may be stale); a failed read is an error, never "no runs".
 */
export function usePaletteRuns(open: boolean): { status: RunsState; runs: PaletteRun[] } {
  const [state, setState] = useState<{ status: RunsState; runs: PaletteRun[] }>({ status: 'loading', runs: [] })
  useEffect(() => {
    if (!open) return
    let alive = true
    setState({ status: 'loading', runs: [] })
    void readPaletteRuns(listRuns).then((r) => alive && setState(r))
    return () => {
      alive = false
    }
  }, [open])
  return state
}

export interface CommandPaletteProps {
  commands: PaletteCommand[]
  runs: RunsState
  mac: boolean
  /** Called once the palette has finished closing (after its exit animation). */
  onClose: () => void
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
  const shown = useMemo(() => filterEntries(commands, query, (g) => t(GROUP_KEY[g])), [commands, query, t])
  const current = activeIndex(shown, activeId)
  const base = useId()
  const optionId = (i: number) => `${base}-opt-${i}`

  useEffect(() => {
    const d = ref.current
    if (d && !d.open && typeof d.showModal === 'function') d.showModal()
    return () => d?.close()
  }, [])
  useEffect(() => {
    if (current >= 0) document.getElementById(optionId(current))?.scrollIntoView?.({ block: 'nearest' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current])

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
    close: () => dismiss(),
  }
  // Keys are handled for the whole dialog, so they work wherever focus sits inside it.
  const onKeyDown = (e: KeyboardEvent<HTMLDialogElement>) => {
    const action = paletteKey({ key: e.key, ctrlKey: e.ctrlKey, metaKey: e.metaKey, shiftKey: e.shiftKey, altKey: e.altKey, repeat: e.repeat, composing: e.nativeEvent.isComposing }, mac)
    if (!action) return
    e.preventDefault()
    act[action]()
  }
  // Pressing anywhere but the field or the list keeps focus in the field; a press that
  // starts on the backdrop is remembered so only a full click there closes the palette.
  const onMouseDown = (e: MouseEvent<HTMLDialogElement>) => {
    const target = e.target as Node
    pressedBackdrop.current = target === ref.current
    if (target !== inputRef.current && !(target instanceof Element && target.closest('[role="listbox"]'))) e.preventDefault()
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
      onClick={(e) => e.target === ref.current && pressedBackdrop.current && dismiss()}
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
          <div className="cmdk-none" role="status" data-testid="palette-none">
            {t('palette.none', { q: query.trim() })}
          </div>
        ) : (
          <Results id={listId} shown={shown} current={current} optionId={optionId} mac={mac} onHover={(i) => setActiveId(shown[i]?.id ?? null)} onChoose={choose} />
        )}
        {searching && runs !== 'ready' && (
          <div className={runs === 'error' ? 'cmdk-note bad' : 'cmdk-note'} role="status" data-testid="palette-runs-state">
            {t(runs === 'error' ? 'palette.runsError' : 'palette.runsLoading')}
          </div>
        )}
      </div>
      <Footer />
    </dialog>
  )
}

interface ResultsProps {
  id: string
  shown: PaletteCommand[]
  current: number
  optionId: (i: number) => string
  mac: boolean
  onHover: (i: number) => void
  onChoose: (i: number) => void
}

function Results({ id, shown, current, optionId, mac, onHover, onChoose }: ResultsProps) {
  const { t } = useI18n()
  const groups = groupRuns(shown)
  return (
    // The listbox scrolls. Focus normally stays in the search field; if the listbox takes it, the dialog's keys still work.
    <div className="cmdk-list" role="listbox" id={id} aria-label={t('palette.results')} aria-activedescendant={current >= 0 ? optionId(current) : undefined} tabIndex={0}>
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
