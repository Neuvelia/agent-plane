import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Button, Card, Field, HelpTip, KPI, ListRow, Pill, SegmentedControl, Tag } from './primitives'

const meta: Meta = { title: 'UI/Primitives' }
export default meta
type S = StoryObj

export const Pills: S = {
  render: () => (
    <div className="row" style={{ padding: 16 }}>
      <Pill tone="n">Pending</Pill>
      <Pill tone="b">Retrying 2/3</Pill>
      <Pill tone="p">Succeeded</Pill>
      <Pill tone="x">Failed</Pill>
      <Pill tone="w">Human</Pill>
      <Pill tone="w" small>Ponawianie 2/3 — długi tekst</Pill>
      <Tag>v12</Tag>
    </div>
  ),
}

export const Buttons: S = {
  render: () => (
    <div className="row" style={{ padding: 16 }}>
      <Button>Default</Button>
      <Button variant="primary">Run graph</Button>
      <Button variant="danger">Reject</Button>
      <Button size="sm">Small</Button>
      <Button variant="primary" disabled>Disabled</Button>
      <Button variant="primary">Uruchom ponownie nieudany węzeł</Button>
    </div>
  ),
}

export const CardWithHeader: S = {
  render: () => (
    <div style={{ maxWidth: 520 }}>
      <Card title="Needs attention" subtitle="2 items" action={<HelpTip>Only what cannot move without you.</HelpTip>}>
        <ListRow>Approval #31</ListRow>
        <ListRow selected onClick={() => undefined}>Run #182</ListRow>
      </Card>
      <Card title="Empty card">
        <div className="mu empty-line">Nothing here</div>
      </Card>
    </div>
  ),
}

export const Segmented: S = {
  render: () => {
    const [v, setV] = useState('map')
    return (
      <div style={{ padding: 16 }}>
        <SegmentedControl label="View" value={v} onChange={setV} segments={[{ value: 'map', label: 'Map' }, { value: 'yaml', label: 'YAML' }, { value: 'split', label: 'Split' }]} />
      </div>
    )
  },
}

export const Kpis: S = {
  render: () => (
    <div className="row" style={{ padding: 16, gap: 32 }}>
      <KPI label="Wall clock" value="48.3s" />
      <KPI label="Retries" value={1} />
      <KPI label="Tokens" unknownLabel="unknown" />
    </div>
  ),
}

export const Fields: S = {
  render: () => (
    <div style={{ padding: 16, maxWidth: 420 }}>
      <Field label="Graph file" htmlFor="story-f"><input id="story-f" placeholder="/path/to/graph.yaml" /></Field>
    </div>
  ),
}
