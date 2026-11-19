import { useEffect, useState } from 'react'

/** Whether this app session's engine records run values; undefined until known or when the setting cannot be read. */
export function useSessionRecording(): boolean | undefined {
  const [active, setActive] = useState<boolean | undefined>(undefined)
  useEffect(() => {
    let alive = true
    window.agentplane
      .recordingSettings()
      .then((s) => alive && setActive(s.active))
      .catch(() => alive && setActive(undefined))
    return () => {
      alive = false
    }
  }, [])
  return active
}
