// Native macOS packaged history smoke. Run after `npm run package:dir`:
// node scripts/history-e2e.cjs [absolute AgentPlane.app path] [screenshot directory]
// Uses a fresh private HOME so the packaged app never touches the user's data.
const assert = require('node:assert/strict')
const { randomUUID } = require('node:crypto')
const { spawn } = require('node:child_process')
const fs = require('node:fs/promises')
const os = require('node:os')
const path = require('node:path')
const net = require('node:net')

const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const deadline = async (name, ms, probe) => {
  const until = Date.now() + ms
  let last
  while (Date.now() < until) {
    try { const value = await probe(); if (value) return value } catch (error) { last = error }
    await delay(100)
  }
  throw new Error(`${name} timed out${last ? `: ${last.message}` : ''}`)
}

class FramedService {
  constructor(binary, stateDir) {
    this.child = spawn(binary, ['--state-dir', stateDir], { stdio: ['pipe', 'pipe', 'pipe'] })
    this.child.stderr.resume()
    this.pending = new Map()
    this.buffer = Buffer.alloc(0)
    this.exit = new Promise((resolve) => this.child.once('exit', (code, signal) => resolve({ code, signal })))
    this.child.stdout.on('data', (chunk) => this.receive(chunk))
    this.child.once('error', (error) => this.rejectAll(error))
    this.child.once('exit', () => this.rejectAll(new Error('Local service exited')))
  }
  rejectAll(error) {
    for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(error) }
    this.pending.clear()
  }
  receive(chunk) {
    this.buffer = Buffer.concat([this.buffer, chunk])
    while (this.buffer.length >= 4) {
      const size = this.buffer.readUInt32BE(0)
      if (!size || size > 8 * 1024 * 1024) { this.rejectAll(new Error('Invalid service frame')); return }
      if (this.buffer.length < size + 4) return
      const body = this.buffer.subarray(4, size + 4)
      this.buffer = this.buffer.subarray(size + 4)
      const response = JSON.parse(body.toString('utf8'))
      const pending = this.pending.get(response.request_id)
      if (!pending) continue
      this.pending.delete(response.request_id)
      clearTimeout(pending.timer)
      if (response.error) pending.reject(new Error(`${response.error.code}: ${response.error.message}`))
      else pending.resolve(response.result)
    }
  }
  request(method, params = {}) {
    const request_id = randomUUID()
    const body = Buffer.from(JSON.stringify({ protocol_version: 1, request_id, method, params }))
    const header = Buffer.alloc(4)
    header.writeUInt32BE(body.length)
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(request_id); reject(new Error(`${method} timed out`)) }, 15000)
      this.pending.set(request_id, { resolve, reject, timer })
      this.child.stdin.write(Buffer.concat([header, body]), (error) => {
        if (error) { clearTimeout(timer); this.pending.delete(request_id); reject(error) }
      })
    })
  }
  async close() {
    if (!this.child.killed) this.child.stdin.end()
    const result = await Promise.race([this.exit, delay(15000).then(() => null)])
    if (!result) { this.child.kill('SIGTERM'); throw new Error('Local service did not stop after EOF') }
    assert.equal(result.code, 0, 'Local service failed to stop cleanly')
  }
}

async function freePort() {
  const server = net.createServer()
  await new Promise((resolve, reject) => server.once('error', reject).listen(0, '127.0.0.1', resolve))
  const port = server.address().port
  await new Promise((resolve) => server.close(resolve))
  return port
}

class CDP {
  constructor(socket) {
    this.socket = socket
    this.pending = new Map()
    this.nextID = 1
    socket.addEventListener('message', (event) => {
      const message = JSON.parse(event.data)
      const pending = this.pending.get(message.id)
      if (!pending) return
      this.pending.delete(message.id)
      clearTimeout(pending.timer)
      if (message.error) pending.reject(new Error(message.error.message))
      else pending.resolve(message.result)
    })
    socket.addEventListener('close', () => {
      for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(new Error('Renderer debugger disconnected')) }
      this.pending.clear()
    })
  }
  call(method, params = {}) {
    const id = this.nextID++
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(`${method} timed out`)) }, 15000)
      this.pending.set(id, { resolve, reject, timer })
      this.socket.send(JSON.stringify({ id, method, params }))
    })
  }
  async evaluate(expression) {
    const result = await this.call('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true })
    if (result.exceptionDetails) throw new Error(`Renderer evaluation failed: ${result.exceptionDetails.exception?.description || result.exceptionDetails.text}`)
    return result.result.value
  }
  close() { this.socket.close() }
}

async function connectCDP(port) {
  const target = await deadline('packaged renderer', 15000, async () => {
    const response = await fetch(`http://127.0.0.1:${port}/json/list`)
    if (!response.ok) return null
    return (await response.json()).find((item) => item.type === 'page' && item.webSocketDebuggerUrl)
  })
  const socket = new WebSocket(target.webSocketDebuggerUrl)
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once: true })
    socket.addEventListener('error', reject, { once: true })
  })
  return new CDP(socket)
}

async function launchApp(appBinary, isolatedHome) {
  const port = await freePort()
  const child = spawn(appBinary, [`--remote-debugging-address=127.0.0.1`, `--remote-debugging-port=${port}`], {
    env: { ...process.env, HOME: isolatedHome }, stdio: ['ignore', 'ignore', 'pipe']
  })
  child.stderr.resume()
  const exit = new Promise((resolve) => child.once('exit', (code, signal) => resolve({ code, signal })))
  try {
    const cdp = await connectCDP(port)
    await deadline('local history UI', 15000, async () =>
      (await cdp.evaluate('document.body.innerText')).includes('Recent history'))
    return { child, exit, cdp }
  } catch (error) {
    child.kill('SIGTERM')
    await exit
    throw error
  }
}

async function closeApp(app) {
  app.cdp.close()
  app.child.kill('SIGTERM')
  const result = await Promise.race([app.exit, delay(15000).then(() => null)])
  assert.ok(result, 'Packaged app did not stop')
}

async function capture(cdp, outputDir, name) {
  if (!outputDir) return
  await fs.mkdir(outputDir, { recursive: true })
  const result = await cdp.call('Page.captureScreenshot', { format: 'png' })
  await fs.writeFile(path.join(outputDir, name), Buffer.from(result.data, 'base64'))
}

async function inspect(cdp, runID, outputDir, screenshotName) {
  await deadline('historical row', 15000, async () =>
    cdp.evaluate(`!!document.querySelector('button[aria-label="Inspect run ${runID}"]')`))
  await cdp.evaluate(`document.querySelector('button[aria-label="Inspect run ${runID}"]').click()`)
  await deadline('historical detail', 15000, async () => {
    const text = await cdp.evaluate('document.querySelector("section.history-detail")?.innerText || ""')
    return text.includes('Storage') && text.includes('complete') && text.includes('Retained events')
  })
  await capture(cdp, outputDir, screenshotName)
}

async function run() {
  assert.equal(process.platform, 'darwin', 'This smoke runs only on macOS')
  const defaultApp = path.resolve(__dirname, '..', 'dist', `mac-${process.arch}`, 'AgentPlane.app')
  const appPath = path.resolve(process.argv[2] || defaultApp)
  const appBinary = path.join(appPath, 'Contents', 'MacOS', 'AgentPlane')
  const sidecar = path.join(appPath, 'Contents', 'Resources', 'agentplane', 'bin', `${process.platform}-${process.arch}`, 'agentplane-desktop')
  const outputDir = process.argv[3] ? path.resolve(process.argv[3]) : null
  await fs.access(appBinary)
  await fs.access(sidecar)
  const isolatedHome = await fs.mkdtemp(path.join(os.tmpdir(), 'agentplane-history-e2e-'))
  await fs.chmod(isolatedHome, 0o700)
  const stateDir = path.join(isolatedHome, 'Library', 'Application Support', 'AgentPlane')
  const projectDir = path.join(isolatedHome, 'project')
  const workspaceDir = path.join(projectDir, 'workspace')
  const graphPath = path.join(projectDir, 'graph.yaml')
  let service
  let app
  try {
    await fs.mkdir(workspaceDir, { recursive: true, mode: 0o700 })
    await fs.writeFile(graphPath, `schema_version: 0\nid: history-e2e\ninputs: {}\nnodes:\n  report:\n    type: shell\n    config:\n      command: /usr/bin/printf\n      args: ["history e2e\\n"]\n    outputs: {}\nedges: []\noutputs: {}\n`, { mode: 0o600 })
    service = new FramedService(sidecar, stateDir)
    await service.request('hello', { client_protocol_version: 1 })
    const grant = await service.request('projects.grant', { canonical_path: projectDir })
    const project = await service.request('projects.add', { grant_token: grant.grant_token, path_digest: grant.path_digest })
    const workspace = await service.request('workspaces.add', { project_id: project.project_id, relative_path: 'workspace' })
    const document = await service.request('documents.open', { project_id: project.project_id, relative_path: 'graph.yaml' })
    const selected = { document_id: document.document_id, revision: document.revision, workspace_id: workspace.workspace_id, inputs: {}, max_concurrency: 1 }
    const preflight = await service.request('runs.preflight', selected)
    assert.equal(preflight.has_errors, false, 'Real graph preflight failed')
    assert.ok(preflight.preflight_token && preflight.payload_digest, 'Preflight omitted Start authority')
    const started = await service.request('runs.start', { ...selected, idempotency_key: randomUUID(), preflight_token: preflight.preflight_token, payload_digest: preflight.payload_digest })
    assert.ok(started.run_id, 'Start omitted run identity')
    await deadline('completed archived run', 15000, async () => {
      const page = await service.request('history.list', { limit: 20 })
      return page.runs.find((item) => item.run_id === started.run_id && item.storage_state === 'complete')
    })
    const archived = await service.request('history.get', { run_id: started.run_id })
    assert.equal(archived.run.runtime_state, 'succeeded')
    await service.close()
    service = null

    await fs.rm(projectDir, { recursive: true })
    app = await launchApp(appBinary, isolatedHome)
    await inspect(app.cdp, started.run_id, outputDir, 'history-first-launch.png')
    await closeApp(app)
    app = null

    app = await launchApp(appBinary, isolatedHome)
    await inspect(app.cdp, started.run_id, outputDir, 'history-after-relaunch.png')
    await app.cdp.evaluate('document.querySelector("button[aria-label=Settings]").click()')
    await deadline('retention action', 5000, async () => cdpButtonExists(app.cdp, 'Prune expired runs'))
    await app.cdp.evaluate('[...document.querySelectorAll("button")].find((button) => button.textContent === "Prune expired runs").click()')
    await deadline('retention result', 15000, async () => {
      const text = await app.cdp.evaluate('document.body.innerText')
      if (text.includes('Prune unavailable:')) {
        const probe = await app.cdp.evaluate('(async () => { try { return { result: await window.agentplane.pruneHistory() } } catch (error) { return { code: error?.code, message: error?.message, text: String(error) } } })()')
        const retained = await app.cdp.evaluate(`(async () => (await window.agentplane.listHistory({ limit: 20 })).runs.some((run) => run.run_id === ${JSON.stringify(started.run_id)}))()`)
        throw new Error(`${text.match(/Prune unavailable:[^\n]*/)?.[0] || 'Prune unavailable'}; direct call: ${JSON.stringify(probe)}; history retained: ${retained}`)
      }
      return text.includes('Removed 0 expired runs')
    })
    await capture(app.cdp, outputDir, 'history-prune.png')
    await closeApp(app)
    app = null

    service = new FramedService(sidecar, stateDir)
    await service.request('hello', { client_protocol_version: 1 })
    const page = await service.request('history.list', { limit: 20 })
    assert.ok(page.runs.some((item) => item.run_id === started.run_id && item.storage_state === 'complete'), 'Prune removed a recent complete run')
    await service.close()
    service = null
    console.log(JSON.stringify({ packaged_app: 'pass', real_run: started.run_id, restart: 'pass', repo_removed: 'pass', inspector: 'pass', prune: 'pass' }))
  } finally {
    if (app) { app.child.kill('SIGTERM'); await app.exit }
    if (service) { service.child.kill('SIGTERM'); await service.exit }
    await fs.rm(isolatedHome, { recursive: true, force: true })
  }
}

async function cdpButtonExists(cdp, label) {
  return cdp.evaluate(`[...document.querySelectorAll("button")].some((button) => button.textContent === ${JSON.stringify(label)})`)
}

run().catch((error) => { console.error(error); process.exitCode = 1 })
