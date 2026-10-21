import type { ProjectView } from '../preload/api'

export type Page = 'dashboard' | 'project' | 'runner' | 'settings'
export type Route = { page: 'project'; projectID: string } | { page: Exclude<Page, 'project'> }

const order: Page[] = ['dashboard', 'project', 'runner', 'settings']

export function routeFromKey(key: string, current: Page): Page | null {
  if (key !== 'ArrowUp' && key !== 'ArrowDown') return null
  const index = order.indexOf(current)
  return order[(index + (key === 'ArrowDown' ? 1 : order.length - 1)) % order.length]
}

export function routeAfterProjects(route: Route, projects: ProjectView[]): Route {
  return route.page === 'project' && !projects.some((item) => item.project_id === route.projectID)
    ? { page: 'dashboard' } : route
}

export function resolveRoute(route: Route, projects: ProjectView[]): Route {
  return routeAfterProjects(route, projects)
}
