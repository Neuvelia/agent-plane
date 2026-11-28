import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { getCapabilities, rowText, toolName, type CapabilityReport } from '../capabilities'
import { useI18n } from '../i18n/context'
import type { MessageKey } from '../i18n'
import { BLANK } from '../setup'
import { noticesFor, SUGGESTED, TEMPLATES, templateDraft, type Notice, type TemplateEntry } from '../templates'
import { Button } from '../ui/primitives'

// F2.12 "New graph" picker: Blank graph plus the bundled templates. A modal
// <dialog> (focus stays inside; Escape and Cancel close it), entries as a native
// radio group (arrow keys move the choice). Notices come from the F2.11 report
// and only inform: no entry is disabled for them.

/** Builds the draft text for the file name the person chose; null when the template is unusable. */
export type DraftMaker = (label: string) => string | null

const usable = (e: TemplateEntry) => templateDraft(e, 'probe.yaml') !== null

export function TemplatePicker({ onChoose, onCancel }: { onChoose: (make: DraftMaker | undefined) => void; onCancel: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  const [choice, setChoice] = useState<string>(BLANK)
  const [report, setReport] = useState<CapabilityReport | null>(null)

  useEffect(() => {
    const d = ref.current
    if (d && !d.open && typeof d.showModal === 'function') d.showModal()
    d?.querySelector<HTMLInputElement>('input[type="radio"]')?.focus()
    return () => d?.close()
  }, [])
  // A report that cannot be read leaves the notices at "not checked"; the picker never waits for it.
  useEffect(() => {
    let alive = true
    void getCapabilities().then((r) => alive && setReport(r.kind === 'ok' ? r.report : null))
    return () => {
      alive = false
    }
  }, [])

  // Closing the dialog before the parent unmounts it is what returns focus to the trigger.
  const cancel = () => {
    ref.current?.close()
    onCancel()
  }
  const submit = () => {
    if (choice === BLANK) {
      ref.current?.close()
      return onChoose(undefined)
    }
    const e = TEMPLATES.find((x) => x.id === choice)
    if (e && usable(e)) {
      ref.current?.close()
      onChoose((label) => templateDraft(e, label))
    }
  }
  return (
    <dialog
      ref={ref}
      className="picker"
      aria-labelledby="picker-title"
      aria-describedby="picker-intro"
      data-testid="template-picker"
      onCancel={(e) => {
        e.preventDefault()
        cancel()
      }}
    >
      <PickerForm report={report} choice={choice} onChoice={setChoice} onSubmit={submit} onCancel={cancel} />
    </dialog>
  )
}

export function PickerForm({ report, choice, onChoice, onSubmit, onCancel }: { report: CapabilityReport | null; choice: string; onChoice: (id: string) => void; onSubmit: () => void; onCancel: () => void }) {
  const { t } = useI18n()
  const selected = TEMPLATES.find((x) => x.id === choice)
  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    onSubmit()
  }
  const onKey = (ev: KeyboardEvent<HTMLInputElement>) => {
    if (ev.key === 'Enter') {
      ev.preventDefault()
      onSubmit()
    }
  }
  return (
    <form onSubmit={submit}>
      <h2 id="picker-title">{t('picker.title')}</h2>
      <p id="picker-intro" className="mu">
        {t('picker.intro')}
      </p>
      <fieldset className="picker-list">
        <legend className="sr-only">{t('picker.legend')}</legend>
        <label className="picker-entry" data-testid="picker-entry-blank">
          <input type="radio" name="start" value={BLANK} checked={choice === BLANK} onChange={() => onChoice(BLANK)} onKeyDown={onKey} aria-describedby="picker-blank-desc" data-testid="template-blank" />
          <span className="picker-text">
            <span className="picker-title">{t('picker.blank.title')}</span>
            <span id="picker-blank-desc" className="mu">
              {t('picker.blank.description')}
            </span>
          </span>
        </label>
        {TEMPLATES.map((e) => (
          <Entry key={e.id} entry={e} report={report} checked={choice === e.id} onPick={() => onChoice(e.id)} onKey={onKey} />
        ))}
      </fieldset>
      <div className="row picker-actions">
        <Button onClick={onCancel} data-testid="picker-cancel">
          {t('picker.cancel')}
        </Button>
        <Button type="submit" variant="primary" disabled={selected !== undefined && !usable(selected)} data-testid="picker-create">
          {t('picker.create')}
        </Button>
      </div>
    </form>
  )
}

function Entry({ entry, report, checked, onPick, onKey }: { entry: TemplateEntry; report: CapabilityReport | null; checked: boolean; onPick: () => void; onKey: (e: KeyboardEvent<HTMLInputElement>) => void }) {
  const { t } = useI18n()
  const descId = `tpl-${entry.id}-desc`
  return (
    <label className="picker-entry" data-testid={`picker-entry-${entry.id}`}>
      <input type="radio" name="start" value={entry.id} checked={checked} onChange={onPick} onKeyDown={onKey} aria-describedby={descId} data-testid={`template-${entry.id}`} />
      <span className="picker-text">
        <span className="picker-title">
          {t(`templates.${entry.id}.title` as MessageKey)}
          {entry.id === SUGGESTED && <span className="tag">{t('picker.suggested')}</span>}
        </span>
        <span id={descId} className="picker-desc">
          <span className="mu">{t(`templates.${entry.id}.description` as MessageKey)}</span>
          <span className="picker-meta">
            <span className="tag">{t('picker.nodes', { types: entry.nodeTypes.join(', ') })}</span>
            <span className="tag">{entry.placeholders > 0 ? t('picker.fill', { count: entry.placeholders }) : t('picker.noFill')}</span>
          </span>
          {!usable(entry) && <span className="picker-notice">{t('picker.unavailable')}</span>}
          {noticesFor(entry, report).map((n, i) => (
            <span key={i} className="picker-notice" data-testid="picker-notice">
              <NoticeText n={n} />
            </span>
          ))}
        </span>
      </span>
    </label>
  )
}

/** One picker notice as a sentence; setup's template cards show the same words. */
export function NoticeText({ n }: { n: Notice }) {
  const { t } = useI18n()
  if (n.kind === 'platform') return <>{t('picker.platform')}</>
  if (n.kind === 'cliOnly') return <>{t('picker.cliOnly')}</>
  const r = n.tool ? rowText(n.tool) : null
  const nm = toolName(n.id)
  const name = 'key' in nm ? t(nm.key) : nm.raw
  const status = !n.checked ? t('picker.notChecked') : !r ? t('picker.notReported') : 'key' in r.status ? t(r.status.key) : r.status.raw
  return <>{t('picker.requires', { tool: name, status })}</>
}
