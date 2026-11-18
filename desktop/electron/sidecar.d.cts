export function binaryName(platform: string): string
export interface SidecarResolution {
  path: string
  source: 'AGENTPLANE_BIN' | 'repo'
  error?: string
}
export function resolveSidecar(deps: {
  platform: string
  repoRoot: string
  env: Record<string, string | undefined>
  exists: (p: string) => boolean
}): SidecarResolution
