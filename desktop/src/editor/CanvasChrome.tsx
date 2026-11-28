import { useReactFlow, useViewport } from '@xyflow/react'
import { useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Glyph, KindGlyph } from './glyphs'
import { NODE_TYPES } from './layout'
import { MOD, Menu, MenuItem, focusForClick, focusForKey, type MenuFocus } from './Menu'

// The chrome floating over the canvas (graph editor v2): view controls at the
// bottom left (zoom, fit, auto-layout, minimap) and tools in a dock at the bottom
// right (select, pan, comment, find a step, Add step). They change the view or
// start an operation; the engine decides every change to the graph.

export type Tool = 'select' | 'pan'

const ZOOM_MS = 200

export function ViewControls({ onArrange, arrangeDisabled, minimap, onMinimap }: { onArrange: () => void; arrangeDisabled: boolean; minimap: boolean; onMinimap: () => void }) {
  const { t } = useI18n()
  const flow = useReactFlow()
  const { zoom } = useViewport()
  return (
    <div className="v2-zoom" role="toolbar" aria-label={t('editor.v2.viewControls')}>
      <button type="button" className="btn ghost ib" aria-label={t('editor.v2.zoomOut')} title={t('editor.v2.zoomOut')} onClick={() => void flow.zoomOut({ duration: ZOOM_MS })} data-testid="zoom-out">
        <Glyph name="minus" />
      </button>
      <span className="pct" aria-live="polite" data-testid="zoom-level">
        {Math.round(zoom * 100)}%
      </span>
      <button type="button" className="btn ghost ib" aria-label={t('editor.v2.zoomIn')} title={t('editor.v2.zoomIn')} onClick={() => void flow.zoomIn({ duration: ZOOM_MS })} data-testid="zoom-in">
        <Glyph name="plus" />
      </button>
      <span className="er-vr" />
      <button type="button" className="btn ghost ib" aria-label={t('editor.v2.fit')} title={t('editor.v2.fit')} onClick={() => void flow.fitView({ padding: 0.2, duration: 300 })} data-testid="zoom-fit">
        <Glyph name="fit" />
      </button>
      <button type="button" className="btn ghost ib" aria-label={t('editor.arrange')} title={t('editor.arrange')} onClick={onArrange} disabled={arrangeDisabled} data-testid="op-arrange">
        <Glyph name="layout" />
      </button>
      <button type="button" className={`btn ghost ib${minimap ? ' on' : ''}`} aria-label={t('editor.v2.minimap')} aria-pressed={minimap} title={t('editor.v2.minimap')} onClick={onMinimap} data-testid="minimap-toggle">
        <Glyph name="map" />
      </button>
    </div>
  )
}

interface DockProps {
  tool: Tool
  onTool: (t: Tool) => void
  nodes: { id: string; type: string }[]
  selected: string | null
  onPick: (id: string) => void
  onAdd: (type: string) => void
  readOnly: boolean
}

export function Dock({ tool, onTool, nodes, selected, onPick, onAdd, readOnly }: DockProps) {
  const { t } = useI18n()
  const findRef = useRef<HTMLButtonElement>(null)
  const addRef = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState<'find' | 'add' | null>(null)
  const [addFocus, setAddFocus] = useState<MenuFocus>('container')
  const openAdd = (focus: MenuFocus) => {
    setAddFocus(focus)
    setOpen('add')
  }
  const [query, setQuery] = useState('')
  const close = (refocus?: boolean) => {
    const which = open
    setOpen(null)
    setQuery('')
    if (refocus) (which === 'add' ? addRef : findRef).current?.focus()
  }
  const shown = nodes.filter((n) => n.id.toLowerCase().includes(query.trim().toLowerCase()))
  return (
    <div className="v2-dock" role="toolbar" aria-label={t('editor.v2.tools')}>
      <button type="button" className={`btn ghost ib${tool === 'select' ? ' on' : ''}`} aria-pressed={tool === 'select'} aria-label={t('editor.v2.toolSelect')} title={`${t('editor.v2.toolSelect')} (V)`} onClick={() => onTool('select')} data-testid="tool-select">
        <Glyph name="select" size={18} />
      </button>
      <button type="button" className={`btn ghost ib${tool === 'pan' ? ' on' : ''}`} aria-pressed={tool === 'pan'} aria-label={t('editor.v2.toolPan')} title={t('editor.v2.toolPanHint')} onClick={() => onTool('pan')} data-testid="tool-pan">
        <Glyph name="pan" size={18} />
      </button>
      <button type="button" className="btn ghost ib soon" aria-disabled="true" aria-label={t('editor.v2.toolCommentSoon')} title={t('editor.v2.toolCommentSoon')} data-testid="tool-comment">
        <Glyph name="comment" size={18} />
      </button>
      <span className="er-vr" />
      <button ref={findRef} type="button" className={`btn ghost ib${open === 'find' ? ' on' : ''}`} aria-haspopup="menu" aria-expanded={open === 'find'} aria-label={t('editor.v2.find')} title={`${t('editor.v2.find')} (${MOD}F)`} onClick={() => (open === 'find' ? close() : setOpen('find'))}
        onKeyDown={(e) => {
          if (open === 'find' || !focusForKey(e.key)) return
          e.preventDefault()
          setOpen('find')
        }}
        data-testid="find-step"
      >
        <Glyph name="search" size={18} />
      </button>
      <span className="er-vr" />
      <button ref={addRef} type="button" className={`btn${open === 'add' ? ' open' : ''}`} aria-haspopup="menu" aria-expanded={open === 'add'} disabled={readOnly} onClick={(e) => (open === 'add' ? close() : openAdd(focusForClick(e)))}
        onKeyDown={(e) => {
          const focus = open === 'add' ? null : focusForKey(e.key)
          if (!focus) return
          e.preventDefault()
          openAdd(focus)
        }}
        data-testid="add-step"
      >
        <Glyph name="plus" size={14} width={2.4} />
        {t('editor.v2.addStep')}
      </button>

      <Menu
        open={open === 'find'}
        anchor={findRef.current ? { kind: 'element', el: findRef.current, align: 'end' } : null}
        onClose={(o) => close(o?.refocus)}
        label={t('editor.v2.find')}
        width={280}
        initialFocus="input"
        testId="find-menu"
        search={
          <>
            <div className="v2-ms">
              <label className="v2-find">
                <Glyph name="search" />
                <input type="text" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('editor.v2.findPlaceholder')} aria-label={t('editor.v2.findPlaceholder')} data-testid="find-input" />
              </label>
            </div>
            {shown.length === 0 && <p className="v2-mempty mu">{nodes.length === 0 ? t('editor.v2.noSteps') : t('editor.v2.noMatch')}</p>}
          </>
        }
      >
        <div role="group" aria-label={t('editor.nodes')}>
          {shown.map((n) => (
            <MenuItem
              key={n.id}
              kind="radio"
              current={selected === n.id}
              icon={<KindGlyph type={n.type} size={14} />}
              label={<span className="mono">{n.id}</span>}
              hint={(NODE_TYPES as readonly string[]).includes(n.type) ? t(`editor.type.${n.type as (typeof NODE_TYPES)[number]}`) : n.type}
              onSelect={() => {
                close()
                onPick(n.id)
              }}
              testId={`list-node-${n.id}`}
            />
          ))}
        </div>
      </Menu>
      <Menu open={open === 'add'} anchor={addRef.current ? { kind: 'element', el: addRef.current, align: 'end' } : null} onClose={(o) => close(o?.refocus)} label={t('editor.v2.addStep')} width={300} initialFocus={addFocus} testId="add-menu">
        {NODE_TYPES.map((type) => (
          <MenuItem
            key={type}
            icon={<KindGlyph type={type} size={14} />}
            label={
              <span className="v2-mi-two">
                <b>{t(`editor.type.${type}`)}</b>
                <span>{t(`editor.v2.typeHint.${type}`)}</span>
              </span>
            }
            onSelect={() => {
              close()
              onAdd(type)
            }}
            testId={`add-${type}`}
          />
        ))}
      </Menu>
    </div>
  )
}
