import { useI18n } from './i18n/context'
import type { MessageKey } from './i18n'
import { Pill, type Tone } from './ui/primitives'
import type { NodeStatus, RunStatus } from './reducer'

export { Pill }

const NODE: Record<NodeStatus, [MessageKey, Tone]> = {
  pending: ['status.node.pending', 'n'],
  ready: ['status.node.ready', 'n'],
  running: ['status.node.running', 'b'],
  retrying: ['status.node.retrying', 'b'],
  waiting_human: ['status.node.waiting_human', 'w'],
  succeeded: ['status.node.succeeded', 'p'],
  failed: ['status.node.failed', 'x'],
  skipped: ['status.node.skipped', 'n'],
  cancelled: ['status.node.cancelled', 'n'],
}
const RUN: Record<RunStatus, [MessageKey, Tone]> = {
  running: ['status.run.running', 'b'],
  succeeded: ['status.run.succeeded', 'p'],
  failed: ['status.run.failed', 'x'],
  cancelled: ['status.run.cancelled', 'n'],
  interrupted: ['status.run.interrupted', 'n'],
}
const GATE: Record<string, [MessageKey, Tone]> = {
  green: ['status.gate.green', 'p'],
  red: ['status.gate.red', 'x'],
}

export function NodePill({ status, label }: { status: NodeStatus; label?: string }) {
  const { t } = useI18n()
  const [key, tone] = NODE[status]
  return (
    <Pill tone={tone} testId="node-status">
      {label ?? t(key)}
    </Pill>
  )
}

export function RunPill({ status }: { status: string }) {
  const { t } = useI18n()
  const known = RUN[status as RunStatus]
  return (
    <Pill tone={known ? known[1] : 'n'} testId="run-status">
      {known ? t(known[0]) : status}
    </Pill>
  )
}

export function VerdictPill({ verdict }: { verdict: string }) {
  const { t } = useI18n()
  const [key, tone] = GATE[verdict] ?? (['status.gate.unknown', 'n'] as [MessageKey, Tone])
  return <Pill tone={tone}>{t(key)}</Pill>
}

export function OutcomePill({ outcome }: { outcome?: string }) {
  const { t } = useI18n()
  if (!outcome) return <Pill tone="b">{t('status.inProgress')}</Pill>
  const tone: Tone = outcome === 'accepted' ? 'p' : outcome === 'rejected' ? 'w' : outcome === 'cancelled' ? 'n' : 'x'
  // The outcome is a machine value from the engine: shown literally.
  return <Pill tone={tone}>{outcome.replace('_', ' ')}</Pill>
}
