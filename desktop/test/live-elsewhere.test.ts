import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { LIVE_ELSEWHERE_POLL_MS, watchLiveElsewhere } from '../src/liveElsewhere'

// A run live in another process: the stream ended, the store still says running.
describe('watching a run that is live in another process', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  const tick = () => vi.advanceTimersByTimeAsync(LIVE_ELSEWHERE_POLL_MS)

  it('stays quiet while the run is still running', async () => {
    const changed = vi.fn()
    const read = vi.fn().mockResolvedValue('running')
    const stop = watchLiveElsewhere(read, changed)
    await tick()
    await tick()
    expect(read).toHaveBeenCalledTimes(2)
    expect(changed).not.toHaveBeenCalled()
    stop()
  })

  it('reports the change once when the writer finished or died, then stops polling', async () => {
    const changed = vi.fn()
    const read = vi.fn().mockResolvedValueOnce('running').mockResolvedValue('interrupted')
    watchLiveElsewhere(read, changed)
    await tick()
    expect(changed).not.toHaveBeenCalled()
    await tick()
    expect(changed).toHaveBeenCalledTimes(1)
    await tick()
    await tick()
    expect(read).toHaveBeenCalledTimes(2)
    expect(changed).toHaveBeenCalledTimes(1)
  })

  it('retries a read that failed or found no run', async () => {
    const changed = vi.fn()
    const read = vi.fn().mockRejectedValueOnce(new Error('engine busy')).mockResolvedValueOnce(null).mockResolvedValue('succeeded')
    watchLiveElsewhere(read, changed)
    await tick()
    await tick()
    expect(changed).not.toHaveBeenCalled()
    await tick()
    expect(changed).toHaveBeenCalledTimes(1)
  })

  it('does nothing once stopped, even if a read is in flight', async () => {
    const changed = vi.fn()
    let finishRead: (status: string) => void = () => undefined
    const read = vi.fn().mockReturnValue(new Promise<string>((resolve) => (finishRead = resolve)))
    const stop = watchLiveElsewhere(read, changed)
    await tick()
    stop()
    finishRead('succeeded')
    await tick()
    expect(changed).not.toHaveBeenCalled()
    expect(read).toHaveBeenCalledTimes(1)
  })

  it('does not start a second read while one is pending', async () => {
    const read = vi.fn().mockReturnValue(new Promise<string>(() => undefined))
    const stop = watchLiveElsewhere(read, vi.fn())
    await tick()
    await tick()
    await tick()
    expect(read).toHaveBeenCalledTimes(1)
    stop()
  })
})
