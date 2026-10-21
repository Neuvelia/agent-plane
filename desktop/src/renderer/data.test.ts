import { describe, expect, it, vi } from 'vitest'
import type { AgentPlaneAPI, LocalSettings, ProjectView } from '../preload/api'
import { collectPages, updateSetting } from './data'

const project: ProjectView = { project_id: 'a', display_name: 'A', canonical_path: '/a', availability: 'ready' }
const settings: LocalSettings = { revision: 2, theme: 'dark', max_concurrency: 4, history_retention_days: 30, keep_all_history: false }

describe('local data actions', () => {
  it('collects bounded pages and rejects cursor loops', async () => {
    const list = vi.fn().mockResolvedValueOnce({ projects: [project], next_cursor: 'next' })
      .mockResolvedValueOnce({ projects: [], next_cursor: 'next' })
    await expect(collectPages(list, 'projects')).rejects.toThrow('repeated cursor')
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('uses the exact settings revision and propagates conflicts', async () => {
    const api = { updateSettings: vi.fn().mockRejectedValue(Object.assign(new Error('revision conflict'), { code: 'conflict' })) }
    await expect(updateSetting(api as unknown as Pick<AgentPlaneAPI, 'updateSettings'>, settings, 'max_concurrency', 3))
      .rejects.toMatchObject({ code: 'conflict' })
    expect(api.updateSettings).toHaveBeenCalledWith(2, { max_concurrency: 3 })
  })
})
