import type { AgentPlaneAPI, LocalSettings } from '../preload/api'

export async function collectPages<T>(
  load: (limit: number, cursor?: string) => Promise<{ [key: string]: unknown; next_cursor?: string }>,
  key: string,
  limit = 100
): Promise<T[]> {
  const result: T[] = []
  const seen = new Set<string>()
  let cursor: string | undefined
  for (let pages = 0; pages < 100; pages++) {
    const page = await load(limit, cursor)
    const items = page[key]
    if (!Array.isArray(items)) throw new Error('Invalid local page')
    result.push(...items as T[])
    if (!page.next_cursor) return result
    if (seen.has(page.next_cursor)) throw new Error('Local service returned a repeated cursor')
    seen.add(page.next_cursor)
    cursor = page.next_cursor
  }
  throw new Error('Local page limit exceeded')
}

export function updateSetting(
  api: Pick<AgentPlaneAPI, 'updateSettings'>,
  settings: LocalSettings,
  key: keyof LocalSettings,
  value: unknown
): Promise<LocalSettings> {
  if (key === 'theme' || key === 'revision') throw new Error('Setting is read-only')
  return api.updateSettings(settings.revision, { [key]: value })
}
