import { useEffect, useState } from 'react'

// Keeps a closing element mounted for its exit animation (F2.5b). Under
// prefers-reduced-motion the element leaves at once: there is no animation to wait for.

export type Phase = 'open' | 'closing'

export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/** Whether to render the element and in which phase; `exitMs` matches its CSS exit animation. */
export function usePresence(open: boolean, exitMs: number): { mounted: boolean; phase: Phase } {
  const [mounted, setMounted] = useState(open)
  useEffect(() => {
    if (open) {
      setMounted(true)
      return
    }
    if (!mounted) return
    const id = window.setTimeout(() => setMounted(false), prefersReducedMotion() ? 0 : exitMs)
    return () => window.clearTimeout(id)
  }, [open, mounted, exitMs])
  return { mounted: open || mounted, phase: open ? 'open' : 'closing' }
}
