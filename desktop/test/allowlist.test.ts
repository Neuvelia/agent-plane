import { describe, expect, it } from 'vitest'
import { isAllowed, isBodyAllowed } from '../electron/allowlist.cjs'

describe('renderer route allowlist (ADR-0021)', () => {
  it('allows exactly the documented routes', () => {
    const allowed: [string, string][] = [
      ['GET', '/runs'],
      ['POST', '/runs'],
      ['GET', '/runs/4af0e02b-4f3d-44f2-844d-37d46bb03cad'],
      ['POST', '/runs/4af0e02b-4f3d-44f2-844d-37d46bb03cad/decisions/node-run_1.2'],
      ['GET', '/approvals'],
      ['POST', '/graphs/validate'],
      ['POST', '/graphs/open'],
      ['POST', '/graphs/stat'],
      ['POST', '/graphs/edit'],
      ['PUT', '/graphs/save'],
      ['GET', '/capabilities'],
      ['POST', '/capabilities/check'],
    ]
    for (const [m, p] of allowed) expect(isAllowed(m, p), `${m} ${p}`).toBe(true)
  })

  it('never lets the renderer reach the grant routes', () => {
    for (const [m, p] of [
      ['POST', '/graphs/grants'],
      ['DELETE', '/graphs/grants/abc'],
      ['POST', '/graphs/grants/abc'],
      ['GET', '/graphs/grants'],
      ['PUT', '/graphs/grants'],
    ] as [string, string][]) expect(isAllowed(m, p), `${m} ${p}`).toBe(false)
  })

  it('refuses a wrong method for an allowed path', () => {
    for (const [m, p] of [
      ['GET', '/graphs/open'],
      ['POST', '/graphs/save'],
      ['PUT', '/runs'],
      ['DELETE', '/runs'],
      ['PUT', '/graphs/edit'],
      ['GET', '/graphs/validate'],
      ['POST', '/approvals'],
    ] as [string, string][]) expect(isAllowed(m, p), `${m} ${p}`).toBe(false)
  })

  it('refuses paths that climb out of a route or carry more than the route', () => {
    for (const p of [
      '/runs/../graphs/grants',
      '/runs/..',
      '/runs/%2e%2e',
      '/runs/%2e%2e/graphs/grants',
      '/runs/a/../../graphs/grants',
      '/graphs/open/',
      '/graphs/open?x=1',
      '/graphs/open#x',
      '/graphs//open',
      '/graphs/open\n',
      '//graphs/open',
      '/runs/abc/events',
      '/runs/abc/decisions',
      '/runs/abc/decisions/',
      '/runs/abc/decisions/x/y',
      '/runs/a b',
      '/graphs',
      '/',
      '',
    ]) expect(isAllowed('POST', p) || isAllowed('GET', p) || isAllowed('PUT', p), JSON.stringify(p)).toBe(false)
  })

  it('allows the capability routes only with their exact method and path (F2.11)', () => {
    for (const [m, p] of [
      ['GET', '/capabilities/check'],
      ['POST', '/capabilities'],
      ['PUT', '/capabilities'],
      ['DELETE', '/capabilities'],
      ['GET', '/capabilities/'],
      ['POST', '/capabilities/check/'],
      ['GET', '/capabilities/../runs'],
      ['POST', '/capabilities/../runs'],
      ['GET', '/capabilitiesx'],
      ['POST', '/capabilities/checkx'],
      ['GET', '/capabilities?x=1'],
      ['POST', '/capabilities/check?x=1'],
      ['GET', '/capabilities/%2e%2e'],
      ['GET', '//capabilities'],
    ] as [string, string][]) expect(isAllowed(m, p), `${m} ${p}`).toBe(false)
  })

  it('refuses anything that is not a method and a path string', () => {
    for (const [m, p] of [[undefined, '/runs'], ['GET', undefined], [null, null], [1, 2], [{}, []]]) expect(isAllowed(m, p)).toBe(false)
  })
})

describe('renderer request bodies name a graph only by grant or text (ADR-0021, decision 9)', () => {
  it('accepts a run started from a grant and a validation of a grant or of text', () => {
    expect(isBodyAllowed('POST', '/runs', { graph_grant: 'g1', inputs: ['a=b'] })).toBe(true)
    expect(isBodyAllowed('POST', '/graphs/validate', { grant: 'g1' })).toBe(true)
    expect(isBodyAllowed('POST', '/graphs/validate', { text: 'schema_version: 0' })).toBe(true)
  })

  it('refuses a filesystem path on both routes, whatever the key spelling', () => {
    // The Go API decodes JSON keys case-insensitively, so a deny-list of "graph_path" would not hold.
    for (const body of [{ graph_path: '/x.yaml' }, { GRAPH_PATH: '/x.yaml' }, { Graph_Path: '/x.yaml' }, { graph_grant: 'g', graph_path: '/x.yaml' }, { graph_grant: 'g', extra: 1 }]) {
      expect(isBodyAllowed('POST', '/runs', body), JSON.stringify(body)).toBe(false)
    }
    for (const body of [{ path: '/x.yaml' }, { PATH: '/x.yaml' }, { grant: 'g', path: '/x.yaml' }, { text: 't', path: '/x.yaml' }]) {
      expect(isBodyAllowed('POST', '/graphs/validate', body), JSON.stringify(body)).toBe(false)
    }
  })

  it('refuses a body that is not a plain object', () => {
    for (const body of [undefined, null, 'graph_path', 7, ['graph_path']]) {
      expect(isBodyAllowed('POST', '/runs', body)).toBe(false)
      expect(isBodyAllowed('POST', '/graphs/validate', body)).toBe(false)
    }
  })

  it('leaves the other routes to the sidecar', () => {
    expect(isBodyAllowed('POST', '/graphs/open', { grant: 'g1' })).toBe(true)
    expect(isBodyAllowed('PUT', '/graphs/save', { grant: 'g1', base_hash: null, text: 't' })).toBe(true)
    expect(isBodyAllowed('GET', '/runs', undefined)).toBe(true)
  })
})
