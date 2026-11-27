import { useCallback, useEffect, useRef, useState } from 'react'
import { checkCapabilities, getCapabilities, type CapabilityResult } from '../capabilities'
import type { CapabilityView } from '../welcome'

/**
 * The capability report behind the Engine, Agents and About sections (F2.11
 * routes, read only). One read on mount; `check` asks the engine to probe again.
 * Only the latest request may settle the view, so a slow read cannot overwrite
 * a newer check. An unreachable engine is `disconnected`, never an empty list.
 */
export function useEngineReport(): { view: CapabilityView; check: () => void; reload: () => void } {
  const [view, setView] = useState<CapabilityView>({ state: 'loading' })
  const seq = useRef(0)

  const settle = useCallback((result: CapabilityResult, mine: number) => {
    if (mine !== seq.current) return
    if (result.kind === 'ok') setView({ state: 'loaded', report: result.report, refreshing: false })
    else if (result.kind === 'failed') setView({ state: 'failed', status: result.status })
    else setView({ state: 'disconnected' })
  }, [])

  const reload = useCallback(() => {
    const mine = ++seq.current
    setView({ state: 'loading' })
    void getCapabilities().then((r) => settle(r, mine))
  }, [settle])

  useEffect(() => {
    reload()
    return () => {
      seq.current++
    }
  }, [reload])

  const check = useCallback(() => {
    const mine = ++seq.current
    setView((v) => (v.state === 'loaded' ? { ...v, refreshing: true } : { state: 'loading' }))
    void checkCapabilities().then((r) => settle(r, mine))
  }, [settle])

  return { view, check, reload }
}
