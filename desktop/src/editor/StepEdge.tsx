import { BaseEdge, EdgeLabelRenderer, getBezierPath, useReactFlow, type Edge, type EdgeProps } from '@xyflow/react'
import { ErrorMark, WarnMark } from './glyphs'

// An edge carries words, not unlabelled badges (graph editor v2): the output port
// it carries, its type when selected, and an issue mark when the engine reported
// one. The label is HTML so it follows the theme and stays readable at any zoom.

export interface StepEdgeData extends Record<string, unknown> {
  port: string
  type?: string
  issue?: 'error' | 'warning'
  issueText?: string
}

export function StepEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, selected, markerEnd, data }: EdgeProps<Edge<StepEdgeData>>) {
  const [path, x, y] = getBezierPath({ sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition })
  const issue = data?.issue
  // A short edge has no room for words: its label shows when it is selected or carries an issue.
  // A label whose middle would sit on a card is left out too: it would cover the card's text.
  const flow = useReactFlow()
  const onCard = flow.getNodes().some((n) => {
    const w = n.measured?.width ?? 0
    const h = n.measured?.height ?? 0
    return x > n.position.x - 8 && x < n.position.x + w + 8 && y > n.position.y - 32 && y < n.position.y + h + 8
  })
  const room = Math.hypot(targetX - sourceX, targetY - sourceY) > 170 && !onCard
  const shown = data && (room || selected || issue)
  return (
    <>
      <BaseEdge id={id} path={path} markerEnd={markerEnd} className={issue ? `v2-edge ${issue}` : 'v2-edge'} />
      {shown && (
        <EdgeLabelRenderer>
          <span className={`v2-el${selected ? ' sel' : ''}${issue ? ` ${issue}` : ''}`} style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y}px)` }} data-testid="edge-label" title={data!.issueText}>
            {issue === 'error' ? <ErrorMark size={12} /> : issue === 'warning' ? <WarnMark size={12} /> : null}
            {data!.port}
            {selected && data!.type && <span className="mu">· {data!.type}</span>}
            {data!.issueText && <span className="sr-only">{data!.issueText}</span>}
          </span>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

export const edgeTypes = { step: StepEdge }
