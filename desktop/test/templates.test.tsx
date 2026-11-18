import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import recorded from './fixtures/capabilities-linux.json'
import type { CapabilityReport } from '../src/capabilities'
import { newGraphText } from '../src/editor/doc'
import { PickerForm } from '../src/editor/TemplatePicker'
import { catalogues } from '../src/i18n'
import { I18nProvider } from '../src/i18n/context'
import { windows as windowsReport } from '../src/storybook/capabilityFixtures'
import { noticesFor, SUGGESTED, TEMPLATES, templateDraft, TEMPLATE_ORDER } from '../src/templates'

const repo = join(__dirname, '..', '..')
const linux = recorded as CapabilityReport
const windows = windowsReport
const entry = (id: string) => TEMPLATES.find((t) => t.id === id)!
const onDisk = (id: string) => readFileSync(join(repo, '.agents', 'templates', `${id}.yaml`), 'utf8')

describe('bundled templates', () => {
  it('are the four index entries in the picker order, parallel-shell suggested first', () => {
    expect(TEMPLATES.map((t) => t.id)).toEqual(['parallel-shell', 'shell-check-loop', 'human-approval', 'agent-scoped-approval'])
    expect([...TEMPLATE_ORDER]).toEqual(TEMPLATES.map((t) => t.id))
    expect(SUGGESTED).toBe('parallel-shell')
  })

  it('carry exactly the text of the files and the index values', () => {
    const index = JSON.parse(readFileSync(join(repo, '.agents', 'templates', 'index.json'), 'utf8')) as { templates: { id: string; requires: string[]; platforms: string[]; placeholders: unknown[] }[] }
    expect(index.templates.map((t) => t.id).sort()).toEqual([...TEMPLATE_ORDER].sort())
    for (const i of index.templates) {
      const t = entry(i.id)
      expect(t.text, i.id).toBe(onDisk(i.id))
      expect(t.requires, i.id).toEqual(i.requires)
      expect(t.platforms, i.id).toEqual(i.platforms)
      expect(t.placeholders, i.id).toBe(i.placeholders.length)
    }
  })

  it('list the node types their file declares', () => {
    for (const t of TEMPLATES) {
      const declared = new Set([...t.text.matchAll(/^ {4}type: (\w+)\s*$/gm)].map((m) => m[1]!))
      expect(new Set(t.nodeTypes), t.id).toEqual(declared)
    }
  })

  it('have EN and PL title and description', () => {
    for (const t of TEMPLATES) {
      for (const c of Object.values(catalogues)) {
        for (const part of ['title', 'description']) {
          const text = (c as Record<string, string>)[`templates.${t.id}.${part}`]
          expect(text?.trim(), `${t.id}.${part}`).toBeTruthy()
        }
      }
    }
  })
})

describe('template draft', () => {
  it('equals the template text with only the id line changed', () => {
    for (const t of TEMPLATES) {
      const draft = templateDraft(t, 'my-flow.yaml')!
      const before = t.text.split('\n')
      const after = draft.split('\n')
      expect(after.length, t.id).toBe(before.length)
      const changed = before.flatMap((l, i) => (l === after[i] ? [] : [i]))
      expect(changed, t.id).toHaveLength(1)
      expect(before[changed[0]!], t.id).toBe(`id: ${t.id}`)
      expect(after[changed[0]!], t.id).toBe('id: my-flow')
    }
  })

  it('derives the same id as a blank graph for the same file name', () => {
    for (const label of ['fresh.yaml', 'My Flow.yml', '1st try.yaml', 'UPPER_case-ok.YAML', '---.yaml', '日本語.yaml']) {
      const id = /^id: (.+)$/m.exec(newGraphText(label))![1]
      for (const t of TEMPLATES) expect(/^id: (.+)$/m.exec(templateDraft(t, label)!)![1], `${t.id} ${label}`).toBe(id)
    }
  })

  it('is refused unless the text has exactly one id line of the template', () => {
    expect(templateDraft({ id: 'x', text: 'schema_version: 0\n' }, 'a.yaml')).toBeNull()
    expect(templateDraft({ id: 'x', text: 'id: x\nnodes: {}\nid: x\n' }, 'a.yaml')).toBeNull()
    expect(templateDraft({ id: 'x', text: 'id: y\n' }, 'a.yaml')).toBeNull()
    expect(templateDraft({ id: 'x', text: '  id: x\n' }, 'a.yaml')).toBeNull()
    expect(templateDraft({ id: 'x', text: 'id: x\r\nnodes: {}\r\n' }, 'a.yaml')).toBe('id: a\r\nnodes: {}\r\n')
  })
})

describe('notices from the capability report', () => {
  it('say nothing for a runnable template on Linux', () => {
    expect(noticesFor(entry('parallel-shell'), linux)).toEqual([])
    expect(noticesFor(entry('human-approval'), linux)).toEqual([])
  })

  it('warn about the platform when the engine reports a system outside platforms', () => {
    expect(noticesFor(entry('parallel-shell'), windows)).toEqual([{ kind: 'platform' }])
    expect(noticesFor(entry('parallel-shell'), { ...linux, engine: { ...linux.engine, os: 'darwin' } })).toEqual([])
  })

  it('give the status of every required tool, and the CLI-only note for Codex', () => {
    const n = noticesFor(entry('agent-scoped-approval'), linux)
    expect(n.map((x) => x.kind)).toEqual(['requires', 'requires', 'cliOnly'])
    expect(n[0]).toMatchObject({ id: 'git', checked: true, tool: { id: 'git' } })
    expect(n[1]).toMatchObject({ id: 'codex', checked: true, tool: { id: 'codex', status: 'not_configured' } })
  })

  it('are "not checked" without a report, and "not reported" for a tool the report lacks', () => {
    expect(noticesFor(entry('agent-scoped-approval'), null)).toEqual([
      { kind: 'requires', id: 'git', tool: undefined, checked: false },
      { kind: 'requires', id: 'codex', tool: undefined, checked: false },
      { kind: 'cliOnly' },
    ])
    const n = noticesFor(entry('agent-scoped-approval'), { ...linux, tools: linux.tools.filter((t) => t.id !== 'git') })
    expect(n[0]).toMatchObject({ id: 'git', tool: undefined, checked: true })
  })
})

describe('picker markup', () => {
  const render = (locale: 'en' | 'pl', report: CapabilityReport | null) =>
    renderToStaticMarkup(
      <I18nProvider locale={locale}>
        <PickerForm report={report} choice="blank" onChoice={() => undefined} onSubmit={() => undefined} onCancel={() => undefined} />
      </I18nProvider>,
    )
  const entries = (html: string) => [...html.matchAll(/data-testid="picker-entry-([a-z-]+)"/g)].map((m) => m[1])

  it('lists Blank graph and the four templates in order as one radio group with the first checked', () => {
    const html = render('en', linux)
    expect(entries(html)).toEqual(['blank', 'parallel-shell', 'shell-check-loop', 'human-approval', 'agent-scoped-approval'])
    expect(html.match(/type="radio"/g)).toHaveLength(5)
    expect(html.match(/name="start"/g)).toHaveLength(5)
    expect(html.match(/checked=""/g)).toHaveLength(1)
    expect(html).toMatch(/Suggested first graph/)
  })

  it('shows localized notices from the report and "not checked" without one', () => {
    expect(render('en', null)).toMatch(/Needs Git: not checked\./)
    const win = render('en', windows)
    expect(win).toMatch(/Commands in this template need Linux or macOS\./)
    expect(win).toMatch(/Needs Codex: Not supported on this system\./)
    expect(render('en', linux)).toMatch(/Needs Codex: Not configured\./)
    expect(render('en', linux)).toMatch(/Agent nodes run only through agentplane run in this version\./)
    expect(render('pl', null)).toMatch(/Wymaga Git: nie sprawdzono\./)
    expect(render('pl', windows)).toMatch(/Polecenia w tym szablonie wymagają systemu Linux albo macOS\./)
  })

  it('never disables an entry for a notice', () => {
    expect(render('en', windows)).not.toMatch(/<input[^>]*disabled/)
  })

  it('states which templates have values to fill', () => {
    const html = render('en', linux)
    expect(html).toMatch(/1 value to fill before it can run/)
    expect(html.match(/runs as is/g)).toHaveLength(3)
  })
})

describe('recorded story fixture', () => {
  it('is the engine answer for the agent template: one blocking issue at its declared place', () => {
    const index = JSON.parse(readFileSync(join(repo, '.agents', 'templates', 'index.json'), 'utf8')) as { templates: { id: string; expected_diagnostics: { code: string; path: string }[] }[] }
    const declared = index.templates.find((t) => t.id === 'agent-scoped-approval')!.expected_diagnostics
    const recordedAnswer = JSON.parse(readFileSync(join(__dirname, 'fixtures', 'graph-validate-template-agent-scoped-approval.json'), 'utf8')) as { diagnostics: { code: string; location: { path: string } }[] }
    expect(recordedAnswer.diagnostics.map((d) => ({ code: d.code, path: d.location.path }))).toEqual(declared)
  })
})
