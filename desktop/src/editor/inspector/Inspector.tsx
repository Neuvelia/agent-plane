import { useId, useState, type ReactNode } from 'react'
import { useI18n } from '../../i18n/context'
import { Button, Card, Pill, StateMessage, Tag } from '../../ui/primitives'
import type { DocState } from '../doc'
import type { GraphDocument } from '../useGraphDocument'
import { AdvancedSection } from './AdvancedSection'
import { DiagList } from './DiagList'
import { FieldControl } from './FieldControl'
import { LoopSection } from './LoopSection'
import { activeFields, configOp, configPath, diagnosticsAt, diagnosticsExactly, inputSources, nodeDiagnostics } from './model'
import { PortsSection } from './PortsSection'
import { TextCommit } from './TextCommit'
import { useEdits } from './useEdits'

// The node inspector (F2.2). Sections and fields come from the sidecar's
// descriptors for the node's type; values come from the definition the engine
// returned; every change is an operation batch. Read-only while the YAML does not
// parse, uses anchors, or the engine cannot be reached.

interface Props {
  g: GraphDocument
  doc: DocState
  nodeId: string | null
  /** The canvas's own read-only reason (parse error or anchors). */
  readOnly: boolean
  onRenamed: (id: string) => void
}

export function Inspector({ g, doc, nodeId, readOnly, onRenamed }: Props) {
  const { t } = useI18n()
  const a = doc.analysis
  const def = a.definition
  const node = nodeId && def ? def.nodes[nodeId] : undefined
  const edits = useEdits(g, def, nodeId)
  const nameId = useId()

  if (!nodeId || !node || !def) {
    return (
      <Card title={t('inspector.title')} testId="inspector">
        <StateMessage kind="empty" testId="inspector-empty">
          {t('inspector.empty')}
        </StateMessage>
      </Card>
    )
  }
  if (!a.nodeTypes && a.status === 'validating') {
    return (
      <Card title={t('inspector.title')} testId="inspector">
        <StateMessage kind="loading">{t('inspector.loading')}</StateMessage>
      </Card>
    )
  }
  const desc = a.nodeTypes?.[node.type]
  const info = a.nodeInfo?.[nodeId]
  // Busy does not disable controls: disabling the focused control would drop keyboard focus.
  const ro = readOnly || !!a.unreachable
  const typeKey = (['shell', 'agent', 'gate', 'human'] as const).find((x) => x === node.type)
  const diags = nodeDiagnostics(a.diagnostics, nodeId)
  const errors = diags.filter((d) => d.severity === 'error').length
  const warnings = diags.length - errors
  const config = node.config ?? {}
  const shapes = new Set((info?.advanced ?? []).filter((x) => x.reason === 'shape').map((x) => x.key))
  // Diagnostics a section shows by path; without descriptors no section is drawn, so all stay at node level.
  const claimed = (d: (typeof diags)[number]) => {
    const m = /^nodes\.[^.]+\.(config|inputs|outputs|loop)(\.|\[|$)/.exec(d.location?.path ?? '')
    return !!desc && !!m && (m[1] !== 'loop' || !!desc.loop)
  }
  const others = Object.keys(def.nodes).filter((id) => id !== nodeId)

  return (
    <Card
      testId="inspector"
      className="ins-card"
      title={
        <span className="row">
          <span className="mono" data-testid="inspector-node">
            {nodeId}
          </span>
          <Tag>{node.type}</Tag>
        </span>
      }
      subtitle={t('inspector.typeLabel', { type: typeKey ? t(`editor.type.${typeKey}`) : node.type })}
      action={
        <span className="row">
          {errors > 0 ? <Pill tone="x" testId="inspector-status">{'✖ '}{t('editor.nodeErrors', { count: errors })}</Pill> : warnings > 0 ? <Pill tone="w" testId="inspector-status">{'⚠ '}{t('editor.nodeWarnings', { count: warnings })}</Pill> : <Pill tone="p" testId="inspector-status">{t('inspector.valid')}</Pill>}
        </span>
      }
    >
      <div className="ins" aria-busy={edits.busy || a.status === 'validating'}>
        {/* The node's name: a field that commits on Enter or blur (graph editor v2). */}
        <div className="er-f ins-name">
          <label htmlFor={nameId}>{t('inspector.name')}</label>
          <TextCommit
            id={nameId}
            value={nodeId}
            className="mono"
            disabled={ro}
            onCommit={(s) => {
              const next = s.trim()
              if (!next) return false
              return edits.apply([{ op: 'rename_node', id: nodeId, new_id: next }], 'rename').then((ok) => {
                if (ok) onRenamed(next)
                return ok
              })
            }}
            testId="inspector-rename-input"
          />
        </div>
        {readOnly && (
          <p className="ins-note" role="status" data-testid="inspector-readonly">
            {a.parseError ? t('inspector.readonly.parse') : t('inspector.readonly.anchors')}
          </p>
        )}
        {a.status === 'validating' && (
          <p className="mu" role="status" data-testid="inspector-checking">
            {t('inspector.checking')}
          </p>
        )}
        {a.unreachable && (
          <p className="ins-note" role="status">
            {t('inspector.unreachable')}
          </p>
        )}
        <DiagList diags={diags.filter((d) => !claimed(d))} refusal={edits.refusal('rename')} />
        {others.length > 0 && info && (
          <p className="mu" data-testid="independence">
            {info.independent_of.length > 0 ? t('inspector.independent', { nodes: info.independent_of.join(', ') }) : t('inspector.dependent')}
          </p>
        )}
        {edits.pending && (
          <div className="banner warn ins-preview" role="alertdialog" aria-label={t('inspector.preview.title')} data-testid="port-preview">
            <div>
              <b>{t('inspector.preview.title')}</b>
              <ul>
                {edits.pending.changes.map((c, i) => (
                  <li key={i}>
                    {t(`inspector.preview.${c.how}`, { what: t(`inspector.preview.what.${c.what}`) })} <span className="mono">{c.text}</span>
                  </li>
                ))}
              </ul>
            </div>
            <span className="row">
              <Button size="sm" variant="primary" onClick={() => void edits.confirm()} data-testid="port-preview-apply">
                {t('inspector.preview.apply')}
              </Button>
              <Button size="sm" onClick={edits.cancel} data-testid="port-preview-cancel">
                {t('editor.cancel')}
              </Button>
            </span>
          </div>
        )}

        {!desc ? (
          <p className="ins-note" role="note" data-testid="inspector-unknown-type">
            {a.nodeTypes ? t('inspector.unknownType', { type: node.type }) : t('inspector.noDescriptors')}
          </p>
        ) : (
          <>
            {/* Graph editor v2 order: what the step does, how it retries, then its connections. */}
            <Section title={t('inspector.section.execution')} testId="section-execution">
              <DiagList diags={diagnosticsExactly(a.diagnostics, `nodes.${nodeId}.config`)} />
              {activeFields(desc.config, config).map((f) => (
                <FieldControl key={f.id} field={f} value={config[f.key!]} present={f.key! in config} inputs={node.inputs ?? {}} diags={diagnosticsAt(a.diagnostics, configPath(nodeId, f.key!))} refusal={edits.refusal(`config.${f.key}`)} shape={shapes.has(f.key!)} readOnly={ro} onCommit={(v) => edits.apply([configOp(nodeId, f.key!, v)], `config.${f.key}`)} />
              ))}
            </Section>
            {desc.loop && (
              <Section title={t('inspector.section.loop')} testId="section-loop">
                <LoopSection g={g} nodeId={nodeId} node={node} desc={desc} info={info} diags={a.diagnostics} readOnly={ro} edits={edits} />
              </Section>
            )}
            <Section title={t('inspector.section.inputs')}>
              <PortsSection def={def} nodeId={nodeId} node={node} desc={desc} direction="input" sources={inputSources(def, nodeId, desc)} diags={a.diagnostics} readOnly={ro} edits={edits} />
            </Section>
            <Section title={t('inspector.section.outputs')}>
              <PortsSection def={def} nodeId={nodeId} node={node} desc={desc} direction="output" diags={a.diagnostics} readOnly={ro} edits={edits} />
            </Section>
          </>
        )}
        <Disclosure title={t('inspector.section.advanced')} testId="section-advanced" initiallyOpen={(info?.advanced ?? []).length > 0}>
          <AdvancedSection entries={info?.advanced ?? []} readOnly={ro} edits={edits} prefix="adv" onSet={(key, value) => [configOp(nodeId, key, value)]} />
        </Disclosure>
      </div>
    </Card>
  )
}

function Section({ title, children, testId }: { title: string; children: ReactNode; testId?: string }) {
  return (
    <section className="ins-section" data-testid={testId}>
      <h3 className="ins-head">{title}</h3>
      {children}
    </section>
  )
}

/** A section that opens on demand (graph editor v2: "Advanced"); open from the start when it has something to show. */
function Disclosure({ title, children, testId, initiallyOpen }: { title: string; children: ReactNode; testId?: string; initiallyOpen: boolean }) {
  const [open, setOpen] = useState(initiallyOpen)
  const id = useId()
  return (
    <section className={`ins-section ins-disc${open ? ' open' : ''}`} data-testid={testId}>
      <h3 className="ins-head">
        <button type="button" className="er-disc" aria-expanded={open} aria-controls={id} onClick={() => setOpen((o) => !o)} data-testid={testId ? `${testId}-toggle` : undefined}>
          {title}
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="m6 9 6 6 6-6" />
          </svg>
        </button>
      </h3>
      <div id={id} className="ins-disc-body" hidden={!open}>
        {children}
      </div>
    </section>
  )
}
