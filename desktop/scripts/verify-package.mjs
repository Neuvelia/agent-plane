import { createHash } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import { existsSync, lstatSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import asar from '@electron/asar'

const desktop = join(dirname(fileURLToPath(import.meta.url)), '..')
const platform = `${process.platform}-${process.arch}`
const executable = process.platform === 'win32' ? 'agentplane-desktop.exe' : 'agentplane-desktop'
const sourceSidecar = join(desktop, 'build', 'sidecar', platform, executable)
const unpacked = process.platform === 'darwin'
  ? join(desktop, 'dist', process.arch === 'arm64' ? 'mac-arm64' : 'mac', 'AgentPlane.app', 'Contents', 'Resources')
  : join(desktop, 'dist', process.platform === 'win32' ? 'win-unpacked' : 'linux-unpacked', 'resources')
const archive = join(unpacked, 'app.asar')
const packagedSidecar = join(unpacked, 'agentplane', 'bin', platform, executable)

for (const path of [sourceSidecar, archive, packagedSidecar]) {
  if (!existsSync(path)) throw new Error(`Missing package artifact: ${relative(desktop, path)}`)
}

const entries = asar.listPackage(archive, { isPack: false }).map((entry) => entry.replace(/^\//, ''))
const allowed = (entry) => entry === '' || entry === 'out' || entry.startsWith('out/') ||
  entry === 'package.json' || entry === 'THIRD_PARTY_NOTICES.md' ||
  entry === 'licenses' || entry.startsWith('licenses/')
const unexpected = entries.filter((entry) => !allowed(entry))
if (unexpected.length) throw new Error(`Unexpected app.asar entries: ${unexpected.slice(0, 20).join(', ')}`)
for (const required of ['out/main/index.js', 'out/preload/index.cjs', 'out/renderer/index.html',
  'THIRD_PARTY_NOTICES.md', 'licenses/Inter-OFL.txt', 'licenses/Lucide-ISC.txt']) {
  if (!entries.includes(required)) throw new Error(`Missing app.asar entry: ${required}`)
}
if (entries.some((entry) => /(^|\/)(src|scripts|test|tests|fixtures|\.agentplane)(\/|$)|\.pdf$/i.test(entry))) {
  throw new Error('Private context, source, fixtures or tests entered the package')
}

function digest(path) { return createHash('sha256').update(readFileSync(path)).digest('hex') }
const sourceHash = digest(sourceSidecar)
if (sourceHash !== digest(packagedSidecar)) throw new Error('Packaged sidecar differs from host build')
const binary = readFileSync(packagedSidecar)
let detected = ''
if (binary.subarray(0, 4).toString('hex') === 'cffaedfe') {
  const cpu = binary.readUInt32LE(4)
  detected = `darwin-${cpu === 0x0100000c ? 'arm64' : cpu === 0x01000007 ? 'x64' : 'unknown'}`
} else if (binary.subarray(0, 4).toString('hex') === '7f454c46') {
  const cpu = binary.readUInt16LE(18)
  detected = `linux-${cpu === 183 ? 'arm64' : cpu === 62 ? 'x64' : 'unknown'}`
} else if (binary.subarray(0, 2).toString() === 'MZ') {
  const offset = binary.readUInt32LE(0x3c)
  if (binary.subarray(offset, offset + 4).toString('hex') !== '50450000') throw new Error('Invalid PE header')
  const cpu = binary.readUInt16LE(offset + 4)
  detected = `win32-${cpu === 0x8664 ? 'x64' : 'unknown'}`
}
if (detected !== platform) throw new Error(`Sidecar architecture ${detected} does not match ${platform}`)
if (process.platform !== 'win32' && (statSync(packagedSidecar).mode & 0o111) === 0) {
  throw new Error('Packaged sidecar is not executable')
}

// The full Resources tree may contain platform runtime assets supplied by the
// pinned Electron distribution. Every such packaged entry must be an exact
// byte copy from that distribution; only app.asar and one sidecar path are new.
const runtimeResources = process.platform === 'darwin'
  ? join(desktop, 'node_modules', 'electron', 'dist', 'Electron.app', 'Contents', 'Resources')
  : join(desktop, 'node_modules', 'electron', 'dist', 'resources')
function resourceEntries(root) {
  const entries = new Map()
  function visit(folder, prefix) {
    for (const name of readdirSync(folder)) {
      const rel = prefix ? `${prefix}/${name}` : name
      const full = join(folder, name)
      const info = lstatSync(full)
      if (info.isSymbolicLink() || (!info.isDirectory() && !info.isFile())) {
        throw new Error(`Unsupported package resource: ${rel}`)
      }
      entries.set(rel, info.isDirectory() ? 'dir' : 'file')
      if (info.isDirectory()) visit(full, rel)
    }
  }
  visit(root, '')
  return entries
}
const packagedEntries = resourceEntries(unpacked)
const runtimeEntries = resourceEntries(runtimeResources)
const ownedEntries = new Set(['app.asar', 'agentplane', 'agentplane/bin',
  `agentplane/bin/${platform}`, `agentplane/bin/${platform}/${executable}`])
for (const [entry, kind] of packagedEntries) {
  if (ownedEntries.has(entry)) continue
  if (runtimeEntries.get(entry) !== kind) throw new Error(`Unexpected package resource: ${entry}`)
  if (kind === 'file' && digest(join(unpacked, entry)) !== digest(join(runtimeResources, entry))) {
    throw new Error(`Modified Electron runtime resource: ${entry}`)
  }
}
for (const entry of ownedEntries) {
  if (!packagedEntries.has(entry)) throw new Error(`Missing package resource: ${entry}`)
}
const state = mkdtempSync(join(tmpdir(), 'agentplane-package-smoke-'))
try {
  const request = Buffer.from(JSON.stringify({ protocol_version: 1, request_id: 'package-smoke', method: 'hello', params: { client_protocol_version: 1 } }))
  const size = Buffer.alloc(4)
  size.writeUInt32BE(request.length)
  const result = spawnSync(packagedSidecar, ['--state-dir', state], { input: Buffer.concat([size, request]), timeout: 5000, encoding: null })
  if (result.error || result.status !== 0 || result.stderr.length || result.stdout.length < 4) {
    throw new Error(`Packaged sidecar handshake failed: ${result.error ?? result.status}`)
  }
  const responseSize = result.stdout.readUInt32BE()
  if (responseSize !== result.stdout.length - 4) throw new Error('Packaged sidecar emitted a malformed frame')
  const response = JSON.parse(result.stdout.subarray(4).toString())
  if (response.request_id !== 'package-smoke' || response.error || !response.result) {
    throw new Error('Packaged sidecar rejected protocol hello')
  }
} finally {
  rmSync(state, { recursive: true, force: true })
}
console.log(`package: verified ${platform}, ${entries.length} app entries, sidecar sha256 ${sourceHash}`)
