import { randomUUID } from 'node:crypto'
import type { Readable, Writable } from 'node:stream'

export const PROTOCOL_VERSION = 1
export const MAX_FRAME_BYTES = 8 << 20
export const MAX_PENDING_CALLS = 16
export const MAX_QUEUED_BYTES = 16 << 20

export type ChildLike = {
  stdin: Writable
  stdout: Readable
  stderr: Readable
  on(event: 'exit', listener: (code: number | null, signal: NodeJS.Signals | null) => void): unknown
  on(event: 'close', listener: (code: number | null, signal: NodeJS.Signals | null) => void): unknown
  on(event: 'error', listener: (error: Error) => void): unknown
}

type Pending = {
  resolve: (result: unknown) => void
  reject: (error: Error) => void
  timer: ReturnType<typeof setTimeout>
}

type Response = {
  protocol_version: number
  request_id: string
  result?: unknown
  error?: { code: string; message: string; retryable: boolean }
}

export class SidecarError extends Error {
  constructor(readonly code: string, message: string, readonly uncertain = false,
              readonly requestID?: string, readonly retryable = false) {
    super(message)
    this.name = 'SidecarError'
  }
}

function frame(value: unknown): Buffer {
  const body = Buffer.from(JSON.stringify(value), 'utf8')
  if (body.length === 0 || body.length > MAX_FRAME_BYTES) {
    throw new SidecarError('invalid_request', 'Request exceeds protocol frame limit')
  }
  const encoded = Buffer.allocUnsafe(4 + body.length)
  encoded.writeUInt32BE(body.length, 0)
  body.copy(encoded, 4)
  return encoded
}

function responseShape(value: unknown): value is Response {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const object = value as Record<string, unknown>
  const result = Object.hasOwn(object, 'result')
  const error = Object.hasOwn(object, 'error')
  return object.protocol_version === PROTOCOL_VERSION && typeof object.request_id === 'string' &&
    object.request_id.length > 0 && result !== error &&
    (!error || (typeof object.error === 'object' && object.error !== null &&
      typeof (object.error as Record<string, unknown>).code === 'string' &&
      typeof (object.error as Record<string, unknown>).message === 'string' &&
      typeof (object.error as Record<string, unknown>).retryable === 'boolean'))
}

export class SidecarClient {
  private readonly header = Buffer.alloc(4)
  private headerUsed = 0
  private body: Buffer | null = null
  private bodyUsed = 0
  private pending = new Map<string, Pending>()
  private timedOut = new Set<string>()
  private writeTail: Promise<void> = Promise.resolve()
  private queuedBytes = 0
  private disconnected = false
  private closed = false
  private ready = false
  private disconnectListeners = new Set<() => void>()

  constructor(private readonly child: ChildLike, private readonly timeoutMS = 30_000) {
    child.stdout.on('data', (chunk: Buffer) => this.receive(chunk))
    child.stdout.on('end', () => this.disconnect('Local service disconnected'))
    child.stdout.on('error', () => this.disconnect('Local service disconnected'))
    child.stdin.on('error', () => this.disconnect('Local service disconnected'))
    child.stderr.on('data', () => { /* Drain private diagnostics without exposing raw data. */ })
    child.on('exit', () => this.disconnect('Local service exited'))
    child.on('close', () => { this.closed = true; this.disconnect('Local service closed') })
    child.on('error', () => this.disconnect('Local service could not start'))
  }

  onDisconnect(listener: () => void): () => void {
    this.disconnectListeners.add(listener)
    return () => this.disconnectListeners.delete(listener)
  }

  get isReady(): boolean { return this.ready && !this.disconnected }

  async hello(expectedVersion: string, requiredMethods: readonly string[]): Promise<void> {
    if (this.ready) throw new SidecarError('invalid_request', 'Handshake already completed')
    const result = await this.request('hello', { client_protocol_version: PROTOCOL_VERSION })
    if (typeof result !== 'object' || result === null || Array.isArray(result)) {
      this.disconnect('Incompatible local service')
      throw new SidecarError('version_mismatch', 'Incompatible local service')
    }
    const hello = result as Record<string, unknown>
    const methods = hello.methods
    if (hello.protocol_version !== PROTOCOL_VERSION || hello.service_version !== expectedVersion ||
      !Array.isArray(methods) || !requiredMethods.every((method) => methods.includes(method))) {
      this.disconnect('Incompatible local service')
      throw new SidecarError('version_mismatch', 'Incompatible local service')
    }
    this.ready = true
  }

  request(method: string, params: unknown, timeoutMS = this.timeoutMS): Promise<unknown> {
    if (this.disconnected) return Promise.reject(new SidecarError('unavailable', 'Local service disconnected', true))
    if (!this.ready && method !== 'hello') return Promise.reject(new SidecarError('unauthorized', 'Handshake required'))
    if (this.pending.size >= MAX_PENDING_CALLS) return Promise.reject(new SidecarError('busy', 'Too many local requests'))
    if (!Number.isSafeInteger(timeoutMS) || timeoutMS < 1) return Promise.reject(new SidecarError('invalid_request', 'Invalid request timeout'))
    const requestID = randomUUID()
    let data: Buffer
    try {
      data = frame({ protocol_version: PROTOCOL_VERSION, request_id: requestID, method, params })
    } catch (error) {
      return Promise.reject(error)
    }
    if (this.queuedBytes + data.length > MAX_QUEUED_BYTES) {
      return Promise.reject(new SidecarError('busy', 'Local service write queue is full'))
    }
    this.queuedBytes += data.length
    const result = new Promise<unknown>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestID)
        this.timedOut.add(requestID)
        if (this.timedOut.size > 64) this.timedOut.delete(this.timedOut.values().next().value!)
        reject(new SidecarError('unavailable', 'Local service request timed out', true))
      }, timeoutMS)
      this.pending.set(requestID, { resolve, reject, timer })
    })
    this.writeTail = this.writeTail.then(() => this.writeFrame(data, timeoutMS)).catch(() => {
      this.disconnect('Local service write stalled')
    }).finally(() => { this.queuedBytes -= data.length })
    return result
  }

  private writeFrame(data: Buffer, timeoutMS: number): Promise<void> {
    if (this.disconnected) return Promise.reject(new Error('disconnected'))
    return new Promise((resolve, reject) => {
      let writable: boolean
      try { writable = this.child.stdin.write(data) } catch (error) { reject(error); return }
      if (writable) { resolve(); return }
      const cleanup = (): void => {
        clearTimeout(timer)
        this.child.stdin.off('drain', onDrain)
        this.child.stdin.off('error', onError)
        this.child.stdin.off('close', onClose)
      }
      const onDrain = (): void => { cleanup(); resolve() }
      const onError = (): void => { cleanup(); reject(new Error('write failed')) }
      const onClose = (): void => { cleanup(); reject(new Error('pipe closed')) }
      const timer = setTimeout(() => { cleanup(); reject(new Error('write stalled')) }, timeoutMS)
      this.child.stdin.once('drain', onDrain)
      this.child.stdin.once('error', onError)
      this.child.stdin.once('close', onClose)
    })
  }

  private receive(chunk: Buffer): void {
    let offset = 0
    while (offset < chunk.length && !this.disconnected) {
      if (this.body === null) {
        const copied = chunk.copy(this.header, this.headerUsed, offset,
          offset + Math.min(4 - this.headerUsed, chunk.length - offset))
        this.headerUsed += copied
        offset += copied
        if (this.headerUsed < 4) break
        const size = this.header.readUInt32BE(0)
        if (size === 0 || size > MAX_FRAME_BYTES) { this.disconnect('Invalid local service frame'); return }
        this.body = Buffer.allocUnsafe(size)
        this.bodyUsed = 0
      }
      const copied = chunk.copy(this.body, this.bodyUsed, offset,
        offset + Math.min(this.body.length - this.bodyUsed, chunk.length - offset))
      this.bodyUsed += copied
      offset += copied
      if (this.bodyUsed < this.body.length) break
      const body = this.body
      this.body = null
      this.headerUsed = 0
      this.bodyUsed = 0
      let parsed: unknown
      try { parsed = JSON.parse(body.toString('utf8')) } catch { this.disconnect('Invalid local service response'); return }
      if (!responseShape(parsed)) { this.disconnect('Invalid local service response'); return }
      const response = parsed as Response
      const pending = this.pending.get(response.request_id)
      if (!pending) {
        if (this.timedOut.delete(response.request_id)) continue
        this.disconnect('Unexpected local service response')
        return
      }
      this.pending.delete(response.request_id)
      clearTimeout(pending.timer)
      if (response.error) pending.reject(new SidecarError(response.error.code,
        response.error.message, false, response.request_id, response.error.retryable))
      else pending.resolve(response.result)
    }
  }

  private disconnect(message: string): void {
    if (this.disconnected) return
    this.disconnected = true
    this.ready = false
    this.body = null
    this.headerUsed = 0
    this.bodyUsed = 0
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer)
      pending.reject(new SidecarError('unavailable', message, true))
    }
    this.pending.clear()
    this.timedOut.clear()
    for (const listener of this.disconnectListeners) listener()
  }

  async close(): Promise<void> {
    if (this.closed) return
    try { this.child.stdin.end() } catch { /* Still wait for process exit. */ }
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => reject(new SidecarError('unavailable', 'Local service did not close')), this.timeoutMS)
      this.child.on('close', () => { clearTimeout(timer); resolve() })
      if (this.closed) { clearTimeout(timer); resolve() }
    })
    this.disconnect('Local service closed')
  }
}
