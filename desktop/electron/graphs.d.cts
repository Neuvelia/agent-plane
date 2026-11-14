import type { Recents } from './recents.cjs'

export type GraphResult =
  | { ok: true; grant: string; label: string; exists: boolean }
  | { ok: false; code: string; message: string }
  | { canceled: true }

export interface GraphDeps {
  request(method: string, path: string, body?: any): Promise<{ status: number; body: any }>
  showOpen(options: object): Promise<{ canceled: boolean; filePaths: string[] }>
  showSave(options: object): Promise<{ canceled: boolean; filePath?: string }>
  recents: Recents
}

export function createGraphHandlers(deps: GraphDeps): {
  openGraph(): Promise<GraphResult>
  newGraph(): Promise<GraphResult>
  recentGraphs(): { id: string; label: string }[]
  openRecentGraph(id: unknown): Promise<GraphResult>
  touchRecent(grant: unknown): boolean
  closeGraph(grant: unknown): Promise<boolean>
}
