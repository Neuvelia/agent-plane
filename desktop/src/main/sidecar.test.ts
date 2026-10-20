import { EventEmitter } from 'node:events'
import { PassThrough, Writable } from 'node:stream'
import { describe, expect, it, vi } from 'vitest'
import { MAX_FRAME_BYTES, MAX_PENDING_CALLS, PROTOCOL_VERSION, SidecarClient, SidecarError, type ChildLike } from './sidecar'

class FakeChild extends EventEmitter implements ChildLike {
  stdin: Writable = new PassThrough()
  stdout = new PassThrough()
  stderr = new PassThrough()
  requests: Array<Record<string, unknown>> = []
  private incoming = Buffer.alloc(0)

  constructor(stalled = false) {
    super()
    if (stalled) {
      this.stdin = new Writable({ highWaterMark: 1, write() { /* Deliberately no drain. */ } })
    } else {
      this.stdin.on('data', (chunk: Buffer) => {
        this.incoming = Buffer.concat([this.incoming, chunk])
        while (this.incoming.length >= 4) {
          const size = this.incoming.readUInt32BE(0)
          if (this.incoming.length < size + 4) return
          this.requests.push(JSON.parse(this.incoming.subarray(4, size + 4).toString('utf8')))
          this.emit('request')
          this.incoming = this.incoming.subarray(size + 4)
        }
      })
    }
  }

  async waitFor(index: number): Promise<void> {
    if (this.requests[index]) return
    await new Promise<void>((resolve) => this.once('request', resolve))
    if (!this.requests[index]) return this.waitFor(index)
  }

  reply(index: number, result: unknown, overrides: Record<string, unknown> = {}): void {
    const body = Buffer.from(JSON.stringify({ protocol_version: PROTOCOL_VERSION,
      request_id: this.requests[index].request_id, result, ...overrides }))
    const frame = Buffer.alloc(4 + body.length)
    frame.writeUInt32BE(body.length)
    body.copy(frame, 4)
    this.stdout.write(frame.subarray(0, 2))
    this.stdout.write(frame.subarray(2))
  }

  exit(): void { this.emit('exit', 0, null); this.emit('close', 0, null) }
}

async function ready(client: SidecarClient, child: FakeChild): Promise<void> {
  const handshake = client.hello('0.0.0-dev', ['runs.events'])
  await child.waitFor(0)
  child.reply(0, { protocol_version: 1, service_version: '0.0.0-dev', methods: ['hello', 'runs.events'] })
  await handshake
}

describe('private sidecar transport', () => {
  it('frames, handshakes and matches one exact response identity', async () => {
    const child = new FakeChild()
    const client = new SidecarClient(child, 100)
    await ready(client, child)
    const response = client.request('runs.events', { run_id: 'id', after_sequence: 0, limit: 1 })
    await child.waitFor(1)
    child.reply(1, { events: [] })
    await expect(response).resolves.toEqual({ events: [] })
    expect(client.isReady).toBe(true)
    child.exit()
  })

  it('distinguishes a received service rejection from uncertain pipe loss', async () => {
    const child = new FakeChild()
    const client = new SidecarClient(child, 100)
    await ready(client, child)
    const rejected = client.request('runs.start', {})
    await child.waitFor(1)
    child.reply(1, undefined, { error: { code: 'unavailable', message: 'Preparation failed', retryable: true } })
    await expect(rejected).rejects.toMatchObject({ code: 'unavailable', uncertain: false,
      requestID: child.requests[1].request_id, retryable: true })
    const uncertain = client.request('runs.start', {})
    await child.waitFor(2)
    child.stdout.end()
    await expect(uncertain).rejects.toMatchObject({ code: 'unavailable', uncertain: true })
  })

  it('rejects stale same-protocol service and missing required method', async () => {
    for (const hello of [
      { protocol_version: 1, service_version: 'old', methods: ['hello', 'runs.events'] },
      { protocol_version: 1, service_version: '0.0.0-dev', methods: ['hello'] }
    ]) {
      const child = new FakeChild()
      const client = new SidecarClient(child, 100)
      const pending = client.hello('0.0.0-dev', ['runs.events'])
      await child.waitFor(0)
      child.reply(0, hello)
      await expect(pending).rejects.toMatchObject({ code: 'version_mismatch' })
      expect(client.isReady).toBe(false)
    }
  })

  it('disconnects on mismatched, duplicate and wrong-version responses', async () => {
    for (const bad of [
      { request_id: 'another' },
      { protocol_version: 2 }
    ]) {
      const child = new FakeChild()
      const client = new SidecarClient(child, 100)
      await ready(client, child)
      const pending = client.request('runs.events', {})
      await child.waitFor(1)
      child.reply(1, {}, bad)
      await expect(pending).rejects.toMatchObject({ code: 'unavailable' })
      expect(client.isReady).toBe(false)
    }
    const child = new FakeChild()
    const client = new SidecarClient(child, 100)
    await ready(client, child)
    const pending = client.request('runs.events', {})
    await child.waitFor(1)
    child.reply(1, {})
    await pending
    child.reply(1, {})
    expect(client.isReady).toBe(false)
  })

  it('bounds pending calls and rejects them on disconnect', async () => {
    const child = new FakeChild()
    const client = new SidecarClient(child, 1000)
    await ready(client, child)
    const pending = Array.from({ length: MAX_PENDING_CALLS }, () => client.request('runs.events', {}))
    await expect(client.request('runs.events', {})).rejects.toMatchObject({ code: 'busy' })
    child.stderr.write('secret process output')
    child.exit()
    const settled = await Promise.allSettled(pending)
    expect(settled.every((result) => result.status === 'rejected' && result.reason instanceof SidecarError)).toBe(true)
  })

  it('ignores a recognized late reply after timeout so Start may reattach', async () => {
    vi.useFakeTimers()
    try {
      const child = new FakeChild()
      const client = new SidecarClient(child, 20)
      await ready(client, child)
      const uncertain = client.request('runs.start', { idempotency_key: 'same' })
      await child.waitFor(1)
      const rejected = expect(uncertain).rejects.toMatchObject({ code: 'unavailable' })
      await vi.advanceTimersByTimeAsync(21)
      await rejected
      child.reply(1, { run_id: 'run' })
      expect(client.isReady).toBe(true)
      const retry = client.request('runs.start', { idempotency_key: 'same' })
      await child.waitFor(2)
      child.reply(2, { run_id: 'run' })
      await expect(retry).resolves.toEqual({ run_id: 'run' })
    } finally {
      vi.useRealTimers()
    }
  })

  it('disconnects a stalled write without retaining a pending request', async () => {
    const child = new FakeChild(true)
    const client = new SidecarClient(child, 20)
    await expect(client.hello('0.0.0-dev', [])).rejects.toMatchObject({ code: 'unavailable' })
    expect(client.isReady).toBe(false)
  })

  it('joins process exit after protocol disconnect before reporting cleanup', async () => {
    const child = new FakeChild()
    const client = new SidecarClient(child, 100)
    await ready(client, child)
    child.stdout.end()
    await new Promise<void>((resolve) => child.stdout.once('end', resolve))
    let closed = false
    const closing = client.close().then(() => { closed = true })
    expect(child.stdin.writableEnded).toBe(true)
    expect(closed).toBe(false)
    child.exit()
    await closing
    expect(closed).toBe(true)
  })

  it('treats a failed spawn close as terminal even when no exit event fires', async () => {
    const child = new FakeChild()
    const client = new SidecarClient(child, 100)
    const handshake = client.hello('0.0.0-dev', [])
    child.emit('error', new Error('spawn denied'))
    await expect(handshake).rejects.toMatchObject({ code: 'unavailable' })
    child.emit('close', null, null)
    await expect(client.close()).resolves.toBeUndefined()
  })

  it('rejects malformed and oversize inbound frames', async () => {
    for (const payload of [Buffer.from([0, 0, 0, 0]), Buffer.from([0, 128, 0, 1])]) {
      const child = new FakeChild()
      const client = new SidecarClient(child, 100)
      await ready(client, child)
      child.stdout.write(payload)
      expect(client.isReady).toBe(false)
    }
  })

  it('accepts two coalesced replies when the first nearly fills one frame', async () => {
    const child = new FakeChild()
    const client = new SidecarClient(child, 5_000)
    await ready(client, child)
    const first = client.request('runs.events', {})
    const second = client.request('runs.snapshot', {})
    await child.waitFor(2)
    const encode = (id: unknown, result: unknown): Buffer => {
      const body = Buffer.from(JSON.stringify({ protocol_version: 1, request_id: id, result }))
      const framed = Buffer.alloc(4 + body.length)
      framed.writeUInt32BE(body.length)
      body.copy(framed, 4)
      return framed
    }
    const tiny = encode(child.requests[1].request_id, '')
    const large = encode(child.requests[1].request_id,
      'a'.repeat(MAX_FRAME_BYTES - tiny.length - 16))
    const trailing = encode(child.requests[2].request_id, { state: 'running' })
    expect(large.length).toBeLessThanOrEqual(MAX_FRAME_BYTES + 4)
    child.stdout.write(Buffer.concat([large, trailing]))
    await expect(first).resolves.toHaveLength(MAX_FRAME_BYTES - tiny.length - 16)
    await expect(second).resolves.toEqual({ state: 'running' })
  })
})
