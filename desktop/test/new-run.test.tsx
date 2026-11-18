import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { I18nProvider } from '../src/i18n/context'
import { RunConfig } from '../src/views'

const render = (initialGraph?: { grant: string; label: string }) =>
  renderToStaticMarkup(
    <I18nProvider locale="en">
      <RunConfig onStarted={() => undefined} initialGraph={initialGraph} />
    </I18nProvider>,
  )

describe('New run holds a grant and a file name, never a path', () => {
  it('has no field to type or show a filesystem path', () => {
    const out = render()
    expect(out).not.toContain('data-testid="graph-path"')
    expect(out).not.toContain('/path/to/graph.yaml')
    expect(out).toContain('data-testid="choose"')
  })

  it('shows the file name of a graph chosen on the Dashboard and not its grant', () => {
    const out = render({ grant: 'grant-7', label: 'demo-vertical.yaml' })
    expect(out).toContain('demo-vertical.yaml')
    expect(out).not.toContain('grant-7')
  })

  it('cannot validate or start before a graph is chosen', () => {
    const out = render()
    expect(out).toMatch(/<button[^>]*data-testid="validate"[^>]*disabled/)
    expect(out).toMatch(/<button[^>]*data-testid="run"[^>]*disabled/)
  })
})
