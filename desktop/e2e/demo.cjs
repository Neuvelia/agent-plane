'use strict'
// End-to-end check of the real Electron app against the real Go sidecar.
//   xvfb-run -a node e2e/demo.cjs [screenshot-dir]
// Requires `make build` (bin/agentplane) and `npm run build` (dist/).
const { _electron } = require('playwright')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')

const root = path.resolve(__dirname, '..', '..')
const graph = path.join(root, '.agents', 'graphs', 'demo-vertical.yaml')
const shots = process.argv[2]
const work = fs.mkdtempSync(path.join(os.tmpdir(), 'ap-e2e-'))
const env = { ...process.env, AGENTPLANE_BIN: path.join(root, 'bin', 'agentplane'), AGENTPLANE_STORE_DIR: path.join(work, 'runs') }
delete env.NODE_OPTIONS

async function launch() {
  const app = await _electron.launch({
    executablePath: require('electron'),
    args: [path.resolve(__dirname, '..'), '--no-sandbox', '--lang=en-US', `--user-data-dir=${path.join(work, 'user')}`],
    env,
  })
  const page = await app.firstWindow()
  // The renderer may load only its own bundle: record anything else.
  page.external = []
  page.on('request', (r) => { if (!/^(file|data|blob):/.test(r.url())) page.external.push(r.url()) })
  await page.setViewportSize({ width: 1440, height: 900 })
  return { app, page }
}
const shot = async (page, name) => shots && (fs.mkdirSync(shots, { recursive: true }), page.screenshot({ path: path.join(shots, name + '.png') }))
const status = (page, id) => page.locator(`[data-testid="node-${id}"]`).getAttribute('data-status')
const waitStatus = (page, id, want) =>
  page.waitForFunction(([i, w]) => document.querySelector(`[data-testid="node-${i}"]`)?.getAttribute('data-status') === w, [id, want], { timeout: 15000 })

async function startDemo(page) {
  await page.click('[data-testid="nav-new"]')
  await page.fill('[data-testid="graph-path"]', graph)
  await page.click('[data-testid="validate"]')
  await page.waitForSelector('[data-testid="preflight-status"]')
  assert.equal(await page.textContent('[data-testid="preflight-status"]'), 'Ready')
  await page.click('[data-testid="run"]')
  await page.waitForSelector('[data-testid="graph-map"]')
}

;(async () => {
  let { app, page } = await launch()
  try {
    // The renderer has no direct access to the sidecar, filesystem or process.
    const leaked = await page.evaluate(() => ({ require: typeof require, process: typeof process, token: JSON.stringify(window.agentplane).includes('token') }))
    assert.deepEqual(leaked, { require: 'undefined', process: 'undefined', token: false })

    // R3 keyboard: the first Tab stop is the skip link; Enter moves focus into the content.
    await page.keyboard.press('Tab')
    assert.equal(await page.evaluate(() => document.activeElement?.className), 'skip-link')
    await page.keyboard.press('Enter')
    assert.equal(await page.evaluate(() => document.activeElement?.id), 'content')
    // Disabled entries are not tab stops; enabled navigation entries are real buttons.
    assert.equal(await page.evaluate(() => [...document.querySelectorAll('.nav.disabled')].every((n) => n.tabIndex < 0 && n.tagName === 'DIV')), true)

    // R0 shell: deferred surfaces are inert, language switches without restart.
    assert.equal(await page.getAttribute('[data-testid="nav-projects"]', 'aria-disabled'), 'true')
    for (const id of ['projects', 'runners', 'docs']) await page.click(`[data-testid="nav-${id}"]`, { force: true })
    assert.equal(await page.isVisible('[data-testid="dashboard"]'), true, 'a disabled entry must not navigate')
    assert.match(await page.textContent('[data-testid="nav-runs"]'), /Runs/)
    assert.equal(await page.textContent('[data-testid="profile"]'), 'KW')
    assert.equal(await page.isVisible('[data-testid="nav-approvals"]'), true)
    await page.click('[data-testid="nav-settings"]')
    await page.click('button:has-text("PL ·")')
    assert.match(await page.textContent('[data-testid="nav-runs"]'), /Uruchomienia/)
    assert.equal(await page.evaluate(() => document.documentElement.lang), 'pl')
    await page.click('button:has-text("EN ·")')
    assert.match(await page.textContent('[data-testid="nav-runs"]'), /Runs/)
    await page.click('[data-testid="nav-runs"]')

    // 1. Run: two branches in parallel, one red -> retry, then a human gate.
    await startDemo(page)
    await waitStatus(page, 'approve', 'waiting_human')
    assert.equal(await status(page, 'fast'), 'succeeded')
    assert.equal(await status(page, 'flaky'), 'succeeded')
    assert.equal(await status(page, 'apply'), 'pending')
    assert.equal(await page.textContent('[data-testid="run-status"]'), 'Running')
    assert.equal(await page.textContent('[data-testid="approvals-badge"]'), '1')
    await shot(page, '1-waiting-for-human')

    // R3 motion: the flowing edge animates normally and stops under reduced motion.
    await page.waitForSelector('.react-flow__edge.animated path', { state: 'attached' })
    // Regression: React Flow draws no edges at all if node objects are replaced per event batch.
    assert.equal(await page.locator('.react-flow__edge').count(), 3)
    const edgeAnimation = () => page.evaluate(() => getComputedStyle(document.querySelector('.react-flow__edge.animated path')).animationName)
    assert.notEqual(await edgeAnimation(), 'none')
    await page.emulateMedia({ reducedMotion: 'reduce' })
    assert.equal(await edgeAnimation(), 'none')
    await page.emulateMedia({ reducedMotion: 'no-preference' })

    // 2. Only the failing node was retried; its red check has reason + evidence.
    await page.click('[data-testid="node-flaky"]')
    // One attempt is shown at a time; the segmented control picks it (latest by default).
    const attempt = page.locator('[data-testid="attempt"]')
    assert.equal(await attempt.count(), 1)
    assert.match(await attempt.textContent(), /accepted/)
    await page.click('[data-testid="attempt-tab-1"]')
    assert.match(await attempt.textContent(), /rejected/)
    assert.match(await attempt.locator('[data-testid="check-reason"]').textContent(), /predicate mismatch/)
    await page.click('[data-testid="attempt-tab-2"]')
    assert.match(await attempt.textContent(), /accepted/)
    // View switch: YAML replaces the map, Split shows both.
    await page.click('[data-testid="view-yaml"]')
    assert.match(await page.textContent('[data-testid="graph-yaml"]'), /demo-vertical/)
    assert.equal(await page.locator('[data-testid="graph-map"]').count(), 0)
    await page.click('[data-testid="view-split"]')
    await page.waitForSelector('[data-testid="graph-map"]')
    assert.equal(await page.locator('[data-testid="graph-yaml"]').count(), 1)
    await page.click('[data-testid="view-map"]')
    await page.click('[data-testid="node-fast"]')
    assert.match(await page.textContent('[data-testid="node-panel"]'), /no correction loop/)
    await page.click('[data-testid="node-flaky"]')
    await shot(page, '2-attempts-reason-evidence')

    // R2: run summary, timeline and event log come from the event stream.
    await page.click('[data-testid="node-fast"]')
    assert.match(await page.textContent('[data-testid="run-summary"]'), /Wall clock/)
    assert.match(await page.textContent('[data-testid="run-summary"]'), /Tokens/)
    assert.match(await page.textContent('[data-testid="run-summary"]'), /Retries\s*1/)
    assert.equal(await page.locator('[data-testid="run-timeline"] .tl-row').count(), 3)
    assert.match(await page.textContent('[data-testid="event-log"]'), /approval_requested/)
    await page.click('[data-testid="events-human"]')
    assert.doesNotMatch(await page.textContent('[data-testid="event-log"]'), /node_started/)
    await page.click('[data-testid="events-all"]')

    // The Dashboard lists the pending approval and the active run.
    await page.click('[data-testid="nav-dashboard"]')
    await page.waitForSelector('[data-testid="attention-item"]')
    assert.match(await page.textContent('[data-testid="needs-attention"]'), /Approval/)
    assert.equal(await page.locator('[data-testid="active-run"]').count(), 1)
    assert.equal(await page.locator('[data-testid="outcomes"] .dot.d-b').count() >= 1, true)

    // 3. The graph stops at the human gate until Approve is clicked.
    await page.click('[data-testid="nav-approvals"]')
    await page.waitForSelector('[data-testid="approval-card"]')
    await page.fill('[data-testid="approval-card"] textarea', 'ship it')
    await shot(page, '3-approvals-inbox')
    await page.click('[data-testid="approval-card"] button.primary')
    await page.waitForSelector('[data-testid="no-approvals"]')
    await page.click('[data-testid="nav-runs"]')
    await page.click('[data-testid="run-row"]')
    await waitStatus(page, 'apply', 'succeeded')
    await page.waitForFunction(() => document.querySelector('[data-testid="run-status"]')?.textContent === 'Succeeded')
    await page.click('[data-testid="node-approve"]')
    assert.match(await page.textContent('[data-testid="decision"]'), /approved.*ship it/i)
    await shot(page, '4-succeeded')
    const finishedEvents = await page.textContent('[data-testid="event-count"]')
    await app.close()

    // 4. History survives an application restart and the map is rebuilt from events.
    ;({ app, page } = await launch())
    await page.waitForSelector('[data-testid="dashboard"]')
    await page.click('[data-testid="nav-runs"]')
    await page.waitForSelector('[data-testid="run-row"]')
    assert.equal(await page.locator('[data-testid="run-row"]').count(), 1)
    assert.match(await page.textContent('[data-testid="run-row"]'), /Succeeded/)
    await page.click('[data-testid="run-row"]')
    await waitStatus(page, 'apply', 'succeeded')
    await page.waitForFunction(() => document.querySelector('[data-testid="run-status"]')?.textContent === 'Succeeded')
    assert.equal(await page.textContent('[data-testid="event-count"]'), finishedEvents)
    // Summary, timeline and event log are rebuilt from the stored events too.
    assert.match(await page.textContent('[data-testid="run-summary"]'), /Wall clock/)
    assert.match(await page.textContent('[data-testid="event-log"]'), /run_finished/)
    await page.click('[data-testid="node-flaky"]')
    // Both attempts are rebuilt from the stored events (one tab each).
    assert.equal(await page.locator('[data-testid^="attempt-tab-"]').count(), 2)
    await shot(page, '5-after-restart')

    // 5. Reject: the run fails, the dependent node is skipped.
    await startDemo(page)
    await waitStatus(page, 'approve', 'waiting_human')
    await page.click('[data-testid="node-approve"]')
    await page.click('[data-testid="approval-box"] button.danger')
    await page.waitForFunction(() => document.querySelector('[data-testid="run-status"]')?.textContent === 'Failed')
    assert.equal(await status(page, 'approve'), 'failed')
    assert.equal(await status(page, 'apply'), 'skipped')
    await shot(page, '6-rejected')
    // 6. If the engine dies mid-run the UI says the connection was lost and keeps
    //    the last events it received; it must not invent a run status.
    await startDemo(page)
    await waitStatus(page, 'approve', 'waiting_human')
    // SIGKILL simulates a crash (SIGTERM would be a graceful, complete shutdown).
    // "[a]gentplane" keeps the pattern from matching pkill's own shell.
    require('node:child_process').execSync(`pkill -9 -f "bin/[a]gentplane serve" || true`)
    await page.waitForSelector('[data-testid="connection-lost"]', { timeout: 15000 })
    assert.equal(await page.textContent('[data-testid="run-status"]'), 'Connection lost')
    assert.equal(await status(page, 'approve'), 'waiting_human')
    assert.equal(await status(page, 'apply'), 'pending')
    assert.equal(await page.locator('[data-testid="approval-box"] button.primary').isDisabled(), true)
    await shot(page, '7-connection-lost')
    assert.match(await page.textContent('[data-testid="connection-indicator"]'), /Connection lost/)
    assert.deepEqual(page.external, [], 'no request may leave the app')
    console.log('desktop e2e: OK')
  } finally {
    await app.close().catch(() => {})
    fs.rmSync(work, { recursive: true, force: true })
  }
})().catch((err) => {
  console.error(err)
  process.exit(1)
})
