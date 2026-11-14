import type { NodeStatus, RunStatus } from './reducer'

type Tone = 'n' | 'b' | 'p' | 'x' | 'w'

const NODE: Record<NodeStatus, [string, Tone]> = {
  pending: ['Pending', 'n'],
  ready: ['Ready', 'n'],
  running: ['Running', 'b'],
  retrying: ['Retrying', 'b'],
  waiting_human: ['Human', 'w'],
  succeeded: ['Succeeded', 'p'],
  failed: ['Failed', 'x'],
  skipped: ['Skipped', 'n'],
  cancelled: ['Cancelled', 'n'],
}
const RUN: Record<RunStatus, [string, Tone]> = {
  running: ['Running', 'b'],
  succeeded: ['Succeeded', 'p'],
  failed: ['Failed', 'x'],
  cancelled: ['Cancelled', 'n'],
  interrupted: ['Interrupted', 'n'],
}

export function Pill({ tone, children, testId }: { tone: Tone; children: React.ReactNode; testId?: string }) {
  return (
    <span className={`pill t-${tone}`} data-testid={testId}>
      {children}
    </span>
  )
}

export function NodePill({ status, label }: { status: NodeStatus; label?: string }) {
  const [text, tone] = NODE[status]
  return (
    <Pill tone={tone} testId="node-status">
      {label ?? text}
    </Pill>
  )
}

export function RunPill({ status }: { status: string }) {
  const [text, tone] = RUN[status as RunStatus] ?? [status, 'n' as Tone]
  return (
    <Pill tone={tone} testId="run-status">
      {text}
    </Pill>
  )
}

export function VerdictPill({ verdict }: { verdict: string }) {
  return <Pill tone={verdict === 'green' ? 'p' : verdict === 'red' ? 'x' : 'n'}>{verdict || 'unknown'}</Pill>
}

export function OutcomePill({ outcome }: { outcome?: string }) {
  if (!outcome) return <Pill tone="b">in progress</Pill>
  const tone: Tone = outcome === 'accepted' ? 'p' : outcome === 'rejected' ? 'w' : outcome === 'cancelled' ? 'n' : 'x'
  return <Pill tone={tone}>{outcome.replace('_', ' ')}</Pill>
}
