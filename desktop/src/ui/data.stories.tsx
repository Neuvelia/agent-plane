import type { Meta, StoryObj } from '@storybook/react-vite'
import { CodeView, DotChart, DotTimeline, EventLog, type Dot, type EventRow } from './data'
import { demoYaml } from '../storybook/fixtures'

const meta: Meta = { title: 'UI/Data' }
export default meta
type S = StoryObj

const tones: Dot['tone'][] = ['p', 'p', 'x', 'p', 'w', 'p', 'b']
const dots: Dot[] = tones.map((tone, i) => ({ key: String(i), tone, label: `run ${i + 1}` }))

export const Outcomes: S = {
  render: () => (
    <div style={{ padding: 16 }}>
      <DotChart dots={dots} slots={14} label="Outcomes, last 7 days" />
    </div>
  ),
}
export const OutcomesEmpty: S = {
  render: () => (
    <div style={{ padding: 16 }}>
      <DotChart dots={[]} slots={14} label="Outcomes, last 7 days" />
    </div>
  ),
}

const rows: EventRow[] = [
  { sequence: 1, time: '14:02:10.1', type: 'run_started' },
  { sequence: 2, time: '14:02:10.2', type: 'node_started', node: 'flaky' },
  { sequence: 3, time: '14:02:31.5', type: 'gate_check', node: 'flaky', detail: 'red' },
  { sequence: 4, time: '14:02:59.3', type: 'approval_requested', node: 'approve', tone: 'w' },
]
export const Events: S = { render: () => <div style={{ padding: 16 }}><EventLog rows={rows} emptyLabel="No events" /></div> }
export const EventsEmpty: S = { render: () => <div style={{ padding: 16 }}><EventLog rows={[]} emptyLabel="No events" /></div> }

export const Timeline: S = {
  render: () => (
    <div style={{ padding: 16 }}>
      <DotTimeline
        label="Timeline, one dot per second"
        rows={[
          { name: 'fast', active: Array.from({ length: 30 }, (_, i) => i < 4) },
          { name: 'flaky', active: Array.from({ length: 30 }, (_, i) => i > 2 && i < 25) },
        ]}
      />
    </div>
  ),
}

export const Yaml: S = { render: () => <div style={{ padding: 16 }}><CodeView text={demoYaml} /></div> }
