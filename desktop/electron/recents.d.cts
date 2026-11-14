export const MAX: number
export interface RecentEntry {
  id: string
  label: string
}
export interface Recents {
  list(): RecentEntry[]
  pathOf(id: string): string | undefined
  add(filePath: string): void
  remove(id: string): void
}
export function createRecents(file: string, deps?: { now?: () => number; randomId?: () => string }): Recents
