import { createContext, useContext, useEffect, type ReactNode } from 'react'

// Screens report whether they currently cannot reach the engine; the global bar
// shows "Connection lost" while any of them does. Stateless without a provider.
type Report = (source: string, lost: boolean) => void
const Ctx = createContext<Report>(() => undefined)

export function ConnectionProvider({ report, children }: { report: Report; children: ReactNode }) {
  return <Ctx.Provider value={report}>{children}</Ctx.Provider>
}

export function useConnectionReport(source: string, lost: boolean): void {
  const report = useContext(Ctx)
  useEffect(() => {
    report(source, lost)
    return () => report(source, false)
  }, [report, source, lost])
}
