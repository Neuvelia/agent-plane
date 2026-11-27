import { useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import type { DocState } from './doc'
import { Glyph } from './glyphs'
import { MOD, Menu, MenuHeading, MenuItem, MenuSep } from './Menu'
import type { GraphDocument } from './useGraphDocument'

// The graph menu on the file name (graph editor v2): the button names the current
// graph. Find first, then the recent graphs (the current one ticked, with its step
// count), New and Open; then "This graph" so its actions clearly act on this file;
// Close; Delete last. The renderer knows a granted file by its name only (ADR-0021),
// so renaming, duplicating, revealing, copying the path and deleting the file have
// no support yet: they are shown as "Soon" and do nothing.

const splitLabel = (label: string): { dir: string; name: string } => {
  const i = Math.max(label.lastIndexOf('/'), label.lastIndexOf('\\'))
  return i < 0 ? { dir: '', name: label } : { dir: label.slice(0, i + 1), name: label.slice(i + 1) }
}

const isMac = typeof navigator !== 'undefined' && /Mac/i.test(navigator.platform || navigator.userAgent)

export function GraphMenu({ g, doc, onNew }: { g: GraphDocument; doc: DocState; onNew: () => void }) {
  const { t } = useI18n()
  const button = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const { dir, name } = splitLabel(doc.label)
  const steps = Object.keys(doc.analysis.definition?.nodes ?? {}).length
  const close = (o?: { refocus: boolean }) => {
    setOpen(false)
    setQuery('')
    if (o?.refocus) button.current?.focus()
  }
  const act = (f: () => void) => () => {
    close()
    f()
  }
  const q = query.trim().toLowerCase()
  const others = g.recent.filter((r) => r.label !== doc.label && r.label.toLowerCase().includes(q))
  const showCurrent = doc.label.toLowerCase().includes(q)
  return (
    <>
      <button
        ref={button}
        type="button"
        className={`er-file${open ? ' open' : ''}`}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls="v2-filemenu"
        aria-label={t('editor.v2.graphMenuLabel', { name: doc.label })}
        onClick={() => (open ? close() : setOpen(true))}
        data-testid="graph-menu"
      >
        {dir && <span className="path">{dir}</span>}
        <span className="name">{name}</span>
        <span className="chev">
          <Glyph name="chevronDown" />
        </span>
      </button>
      <Menu
        open={open}
        anchor={button.current ? { kind: 'element', el: button.current } : null}
        onClose={close}
        label={t('editor.v2.graphMenu')}
        width={340}
        id="v2-filemenu"
        testId="graph-menu-list"
        initialFocus="input"
        search={
          <div className="v2-ms">
            <label className="v2-find">
              <Glyph name="search" />
              <input type="text" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('editor.v2.findGraph')} aria-label={t('editor.v2.findGraph')} data-testid="graph-find" />
            </label>
          </div>
        }
      >
        <MenuHeading>{dir ? t('editor.v2.graphsIn', { dir: dir.replace(/[\\/]$/, '') }) : t('editor.recent')}</MenuHeading>
        <div role="group" aria-label={t('editor.recent')}>
          {showCurrent && <MenuItem kind="radio" current icon="check" label={<span className="mono">{name}</span>} hint={t('editor.v2.steps', { count: steps })} onSelect={() => close()} testId="graph-current" />}
          {others.map((r) => (
            <MenuItem key={r.id} kind="radio" icon="file" label={<span className="mono">{splitLabel(r.label).name}</span>} onSelect={act(() => g.openRecent(r.id))} testId="graph-recent" />
          ))}
          <MenuItem icon="plus" label={t('editor.new')} onSelect={act(onNew)} testId="editor-new" />
          <MenuItem icon="folder" label={t('editor.open')} keys={[MOD, 'O']} onSelect={act(g.open)} testId="editor-open" />
        </div>
        <MenuSep />
        <MenuHeading>
          {t('editor.v2.thisGraph')} · <span className="mono">{name}</span>
        </MenuHeading>
        <div role="group" aria-label={t('editor.v2.thisGraph')}>
          <MenuItem icon="pencil" label={t('editor.v2.renameFile')} soon testId="file-rename" />
          <MenuItem icon="copy" label={t('editor.v2.duplicateFile')} soon testId="file-duplicate" />
          <MenuItem icon="reveal" label={isMac ? t('editor.v2.revealMac') : t('editor.v2.reveal')} soon testId="file-reveal" />
          <MenuItem icon="link" label={t('editor.v2.copyPath')} soon testId="file-copy-path" />
          <MenuItem icon="logout" label={t('editor.v2.closeGraph')} onSelect={act(g.close)} testId="editor-close" />
        </div>
        <MenuSep />
        <MenuItem icon="trash" label={t('editor.v2.deleteFile')} danger soon testId="file-delete" />
      </Menu>
    </>
  )
}
