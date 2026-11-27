import { useI18n } from '../i18n/context'
import { ValuePicker } from '../ValuePickerView'
import type { ValuesPicker } from '../useRunValues'
import { useSessionRecording } from '../useSessionRecording'
import { Card, KeyValue, StateMessage, Tag } from '../ui/primitives'
import { sourceType, targetType } from './bindings'
import { edgeValueView, type EdgeValueView } from './edgeValue'
import { EdgeValueBox, ValueFacts } from './EdgeValueBox'
import { parseRef } from './layout'
import type { Definition } from './types'

// The edge inspector (F2.3 requirement 17, E5b.2): what the selected edge or graph
// input binding is, and what the chosen run recorded on it. It reads the engine's
// definition and the values reducer; which state applies is edgeValue.ts.

interface Props {
  def: Definition
  edge: { kind: 'edge' | 'binding'; from: string; to: string }
  picker: ValuesPicker
}

const nodeOf = (ref: string): string => parseRef(ref)?.node ?? ''

export function EdgeInspector({ def, edge, picker }: Props) {
  const { t } = useI18n()
  const recording = useSessionRecording()
  const view = edgeValueView({ runs: picker.runs, runsStatus: picker.runsStatus, resolved: picker.resolved, stream: picker.stream, from: edge.from })
  const declared = (type: string) => (type === '' ? '—' : type)
  return (
    <Card title={t('editor.edgeValue.title')} testId="edge-inspector">
      <div className="ev" data-testid="edge-inspector-body" data-state={view.state}>
        <KeyValue
          rows={[
            { name: t('editor.edgeValue.source'), value: <span className="mono" data-testid="edge-source">{edge.from}</span> },
            { name: t('editor.edgeValue.target'), value: <span className="mono" data-testid="edge-target">{edge.to}</span> },
            { name: t('editor.edgeValue.declared'), value: <span className="mono" data-testid="edge-type">{declared(sourceType(def, edge.from))}{targetType(def, edge.to) !== sourceType(def, edge.from) ? ` → ${declared(targetType(def, edge.to))}` : ''}</span> },
          ]}
        />
        <ValuePicker picker={picker} />
        <State view={view} edge={edge} picker={picker} />
        {recording !== undefined && (
          <p className="mu" data-testid="edge-recording-hint">
            {t(recording ? 'editor.edgeValue.recording.on' : 'editor.edgeValue.recording.off')}
          </p>
        )}
      </div>
    </Card>
  )
}

function State({ view, edge, picker }: { view: EdgeValueView; edge: Props['edge']; picker: ValuesPicker }) {
  const { t } = useI18n()
  const incomplete = picker.stream.lost
  if (view.state === 'loading') return <StateMessage kind="loading" testId="edge-value-state">{t('editor.edgeValue.loading.title')}</StateMessage>
  const node = nodeOf(edge.from)
  const text = stateText(view, t, node)
  return (
    <div data-testid="edge-value-state" data-state={view.state}>
      <h3 className="ev-state-head" data-testid="edge-value-heading">
        {t(`editor.edgeValue.${view.state}.title`)}
        {picker.resolved.differentVersion && view.state !== 'different_version' && <Tag>{t('values.picker.differentVersion')}</Tag>}
      </h3>
      <p role="status" data-testid="edge-value-message">
        {text}
      </p>
      {view.state === 'value_not_carried' && <ValueFacts value={view.value} />}
      {view.state === 'value' && <EdgeValueBox key={`${edge.from}->${edge.to}:${view.value.sequence}`} value={view.value} label={t('editor.edgeValue.region', { name: edge.from })} incomplete={incomplete} />}
      {incomplete && view.state !== 'value' && (
        <p className="mu" data-testid="edge-value-incomplete">
          {t('editor.edgeValue.incomplete')}
        </p>
      )}
    </div>
  )
}

type Translate = ReturnType<typeof useI18n>['t']

function stateText(view: Exclude<EdgeValueView, { state: 'loading' }>, t: Translate, node: string): string {
  switch (view.state) {
    case 'different_version':
      return t(view.chosen ? 'editor.edgeValue.different_version.text' : 'editor.edgeValue.different_version.textNone')
    case 'node_not_finished':
      return t('editor.edgeValue.node_not_finished.text', { node, status: t(`editor.edgeValue.status.${view.status}`) })
    case 'node_failed_or_skipped':
      return t('editor.edgeValue.node_failed_or_skipped.text', { node, status: t(`editor.edgeValue.status.${view.status}`) })
    case 'older_run':
      return t('editor.edgeValue.older_run.text', { version: view.schemaVersion })
    case 'ambiguous':
      return t('editor.edgeValue.ambiguous.text', { first: view.sequences[0], second: view.sequences[1] })
    case 'no_run':
    case 'run_unavailable':
    case 'not_recorded':
    case 'no_value_for_port':
    case 'value_not_carried':
    case 'value':
      return t(`editor.edgeValue.${view.state}.text`)
  }
}
