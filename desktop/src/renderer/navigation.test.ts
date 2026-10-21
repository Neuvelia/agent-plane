import { describe, expect, it } from 'vitest'
import { resolveRoute, routeAfterProjects, routeFromKey } from './navigation'

describe('local navigation', () => {
  it('selects only registered project routes', () => {
    const projects = [{ project_id: 'one', display_name: 'One', canonical_path: '/one', availability: 'ready' as const }]
    expect(resolveRoute({ page: 'project', projectID: 'one' }, projects)).toEqual({ page: 'project', projectID: 'one' })
    expect(routeAfterProjects({ page: 'project', projectID: 'gone' }, projects)).toEqual({ page: 'dashboard' })
  })

  it('allows arrow keys to move among the four primary destinations', () => {
    expect(routeFromKey('ArrowDown', 'dashboard')).toBe('project')
    expect(routeFromKey('ArrowUp', 'dashboard')).toBe('settings')
    expect(routeFromKey('Enter', 'dashboard')).toBeNull()
  })
})
