import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { MAX_OPS_PER_BATCH, queryConnectTargets } from '../src/editor/connect'
import type { Op, OpResult } from '../src/editor/types'
import type { ApplyResult } from '../src/editor/useGraphDocument'

// The engine refuses a batch over its limit (ADR-0021); the renderer must stay under it.
const goText = readFileSync(new URL('../../internal/adapter/yamlgraph/edit/ops.go', import.meta.url), 'utf8')
const ENGINE_MAX_OPS = Number(/const MaxOps = (\d+)/.exec(goText)?.[1])

// A stand-in for the engine: it refuses an oversized batch and otherwise answers each can_connect.
function engine(sizes: number[] = []) {
  return async (ops: Op[]): Promise<ApplyResult> => {
    sizes.push(ops.length)
    if (ops.length > ENGINE_MAX_OPS) return { ok: false, message: `a batch may hold at most ${ENGINE_MAX_OPS} operations, got ${ops.length} (split the batch)` }
    const results: OpResult[] = ops.map((op, index) => ({ index, op: op.op, query: { ok: op.op === 'can_connect' && op.to !== 'n7.inputs.in' } }))
    return { ok: true, results }
  }
}

const inputs = (n: number) => Array.from({ length: n }, (_, i) => `n${i}.inputs.in`)

describe('connect target queries', () => {
  it('the batch limit matches the engine', () => {
    expect(Number.isInteger(ENGINE_MAX_OPS)).toBe(true)
    expect(MAX_OPS_PER_BATCH).toBe(ENGINE_MAX_OPS)
  })

  it('answers for every input of a graph larger than one batch', async () => {
    const all = inputs(ENGINE_MAX_OPS * 2 + 37)
    const sizes: number[] = []
    const a = await queryConnectTargets(engine(sizes), 'src.outputs.out', all)
    expect(a.ok).toBe(true)
    if (!a.ok) return
    expect(Object.keys(a.targets)).toHaveLength(all.length)
    expect(a.targets['n7.inputs.in']).toEqual({ ok: false })
    expect(a.targets[all[all.length - 1]!]).toEqual({ ok: true })
    expect(Math.max(...sizes)).toBeLessThanOrEqual(ENGINE_MAX_OPS)
    expect(sizes).toHaveLength(3)
  })

  it('sends a graph of exactly one batch as one batch', async () => {
    const sizes: number[] = []
    const a = await queryConnectTargets(engine(sizes), 'src.outputs.out', inputs(ENGINE_MAX_OPS))
    expect(a.ok).toBe(true)
    expect(sizes).toEqual([ENGINE_MAX_OPS])
  })

  it('asks nothing when there are no inputs', async () => {
    const sizes: number[] = []
    expect(await queryConnectTargets(engine(sizes), 'src.outputs.out', [])).toEqual({ ok: true, targets: {} })
    expect(sizes).toEqual([])
  })

  it('reports a refused batch instead of returning no targets', async () => {
    const refuse = async (): Promise<ApplyResult> => ({ ok: false, message: 'the engine could not be reached' })
    expect(await queryConnectTargets(refuse, 'src.outputs.out', inputs(3))).toEqual({ ok: false, message: 'the engine could not be reached' })
  })

  it('stops at the first refused chunk and reports it', async () => {
    let calls = 0
    const second = async (ops: Op[]): Promise<ApplyResult> => {
      calls++
      return calls === 2 ? { ok: false, message: 'refused' } : engine()(ops)
    }
    expect(await queryConnectTargets(second, 'src.outputs.out', inputs(ENGINE_MAX_OPS * 3))).toEqual({ ok: false, message: 'refused' })
    expect(calls).toBe(2)
  })
})
