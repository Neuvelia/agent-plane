import { describe, expect, it } from 'vitest'
import { createGraphHandlers, type GraphDeps } from '../electron/graphs.cjs'
import type { Recents } from '../electron/recents.cjs'

function fakeRecents(): Recents & { items: { id: string; path: string }[] } {
  const items: { id: string; path: string }[] = []
  let n = 0
  return {
    items,
    list: () => items.map((i) => ({ id: i.id, label: i.path.split('/').pop() as string })),
    pathOf: (id) => items.find((i) => i.id === id)?.path,
    add: (p) => {
      const at = items.findIndex((i) => i.path === p)
      if (at >= 0) items.splice(at, 1)
      items.unshift({ id: `r${++n}`, path: p })
    },
    remove: (id) => {
      const at = items.findIndex((i) => i.id === id)
      if (at >= 0) items.splice(at, 1)
    },
  }
}

function setup(over: Partial<GraphDeps> = {}) {
  const calls: { method: string; path: string; body?: any }[] = []
  let issued = 0
  const recents = fakeRecents()
  const deps: GraphDeps = {
    request: async (method, path, body) => {
      calls.push({ method, path, body })
      if (method === 'POST' && path === '/graphs/grants') return { status: 201, body: { grant: `grant${++issued}`, label: String(body.path).split('/').pop(), exists: body.mode === 'open' } }
      if (method === 'DELETE') return { status: 204, body: null }
      return { status: 500, body: null }
    },
    showOpen: async () => ({ canceled: false, filePaths: ['/secret/dir/open.yaml'] }),
    showSave: async () => ({ canceled: false, filePath: '/secret/dir/new' }),
    recents,
    ...over,
  }
  return { h: createGraphHandlers(deps), calls, recents }
}

describe('graph handlers (main process)', () => {
  it('opens a graph and returns only a grant and a label, never the path', async () => {
    const { h, calls, recents } = setup()
    const r = await h.openGraph()
    expect(r).toEqual({ ok: true, grant: 'grant1', label: 'open.yaml', exists: true })
    expect(JSON.stringify(r)).not.toContain('/secret')
    expect(calls[0]).toEqual({ method: 'POST', path: '/graphs/grants', body: { path: '/secret/dir/open.yaml', mode: 'open' } })
    expect(recents.items.map((i) => i.path)).toEqual(['/secret/dir/open.yaml'])
  })

  it('creates a graph in create mode and adds .yaml when the person left it out', async () => {
    const { h, calls, recents } = setup()
    const r = await h.newGraph()
    expect(r).toMatchObject({ ok: true, label: 'new.yaml', exists: false })
    expect(calls[0]?.body).toEqual({ path: '/secret/dir/new.yaml', mode: 'create' })
    expect(recents.items).toHaveLength(0) // a new graph is recent only once it is saved
  })

  it('keeps an existing .yml or .yaml extension', async () => {
    for (const [given, want] of [['/d/a.yml', '/d/a.yml'], ['/d/b.YAML', '/d/b.YAML']]) {
      const { h, calls } = setup({ showSave: async () => ({ canceled: false, filePath: given }) })
      await h.newGraph()
      expect(calls[0]?.body.path).toBe(want)
    }
  })

  it('does nothing when a dialog is cancelled', async () => {
    const { h, calls } = setup({ showOpen: async () => ({ canceled: true, filePaths: [] }), showSave: async () => ({ canceled: true }) })
    expect(await h.openGraph()).toEqual({ canceled: true })
    expect(await h.newGraph()).toEqual({ canceled: true })
    expect(calls).toHaveLength(0)
  })

  it('reports a refused grant with the sidecar code, without a path', async () => {
    const { h } = setup({ request: async () => ({ status: 409, body: { error: { code: 'exists', message: 'the file already exists' } } }) })
    expect(await h.newGraph()).toEqual({ ok: false, code: 'exists', message: 'the file already exists' })
    const down = setup({ request: async () => ({ status: 500, body: null }) })
    expect(await down.h.openGraph()).toMatchObject({ ok: false, code: 'unavailable' })
  })

  it('touches the recent list only for a grant main itself issued', async () => {
    const { h, recents } = setup()
    const made = await h.newGraph()
    expect(h.touchRecent('forged')).toBe(false)
    expect(h.touchRecent(42)).toBe(false)
    expect(recents.items).toHaveLength(0)
    expect('grant' in made && h.touchRecent(made.grant)).toBe(true)
    expect(recents.items.map((i) => i.path)).toEqual(['/secret/dir/new.yaml'])
  })

  it('opens a recent graph from main’s own stored path and forgets one that is gone', async () => {
    const { h, calls, recents } = setup()
    await h.openGraph()
    const id = h.recentGraphs()[0]!.id
    expect(JSON.stringify(h.recentGraphs())).not.toContain('/secret')
    expect(await h.openRecentGraph(id)).toMatchObject({ ok: true, label: 'open.yaml' })
    expect(calls.at(-1)?.body).toEqual({ path: '/secret/dir/open.yaml', mode: 'open' })
    expect(await h.openRecentGraph('nope')).toMatchObject({ ok: false, code: 'not_found' })
    expect(await h.openRecentGraph({ evil: true })).toMatchObject({ ok: false, code: 'not_found' })

    const gone = setup({ request: async () => ({ status: 404, body: { error: { code: 'not_found', message: 'the file does not exist' } } }) })
    gone.recents.add('/secret/dir/missing.yaml')
    const goneId = gone.h.recentGraphs()[0]!.id
    expect(await gone.h.openRecentGraph(goneId)).toMatchObject({ ok: false, code: 'not_found' })
    expect(gone.h.recentGraphs()).toEqual([])
    expect(recents.items).toHaveLength(1)
  })

  it('revokes a grant when the document is closed, once, and only its own', async () => {
    const { h, calls } = setup()
    const r = await h.openGraph()
    const grant = 'grant' in r ? r.grant : ''
    expect(await h.closeGraph(grant)).toBe(true)
    expect(calls.at(-1)).toMatchObject({ method: 'DELETE', path: '/graphs/grants/grant1' })
    expect(await h.closeGraph(grant)).toBe(false)
    expect(await h.closeGraph('forged/../x')).toBe(false)
    expect(calls.filter((c) => c.method === 'DELETE')).toHaveLength(1)
  })
})
