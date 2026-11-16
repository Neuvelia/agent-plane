import type { CheckView } from './reducer'
import { VerdictPill } from './status'
import { Tag } from './ui/primitives'

/** One check result as the engine recorded it: verdict, ID, kind, duration, reason and evidence. */
export function CheckResultView({ check }: { check: CheckView }) {
  return (
    <div className="check" data-testid="check-result">
      <div className="row">
        <VerdictPill verdict={check.verdict} />
        <span className="mono">{check.checkId}</span>
        <Tag>{check.kind}</Tag>
        <span className="mu mono">{check.durationMs} ms</span>
      </div>
      <div className="reason" data-testid="check-reason">
        {check.reason}
      </div>
      <EvidenceList check={check} />
    </div>
  )
}

export function EvidenceList({ check }: { check: CheckView }) {
  return (
    <>
      {check.evidence.map((e, i) => (
        <div className="evidence mono" key={i} data-testid="check-evidence">
          <span className="mu">{e.source}</span>
          {e.path && <span> path={e.path}</span>}
          {e.exitCode !== undefined && <span> exit={e.exitCode}</span>}
          {e.stdout && <pre>{e.stdout}</pre>}
          {e.stderr && <pre>{e.stderr}</pre>}
          {e.omitted ? <span className="mu"> +{e.omitted} omitted</span> : null}
        </div>
      ))}
    </>
  )
}
