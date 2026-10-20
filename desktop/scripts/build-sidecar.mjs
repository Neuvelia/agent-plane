import { spawnSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const desktop = join(dirname(fileURLToPath(import.meta.url)), '..')
const repository = join(desktop, '..')
const executable = process.platform === 'win32' ? 'agentplane-desktop.exe' : 'agentplane-desktop'
const target = join(desktop, 'build', 'sidecar', `${process.platform}-${process.arch}`, executable)
mkdirSync(dirname(target), { recursive: true })
const build = spawnSync('go', ['build', '-o', target, './cmd/agentplane-desktop'], {
  cwd: repository, stdio: 'inherit'
})
if (build.error) throw build.error
if (build.status !== 0) process.exit(build.status ?? 1)
