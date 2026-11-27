'use strict'
// node scripts/stories-smoke.cjs   (after `npm run build-storybook`)
// AP_CHROMIUM=/path/to/chrome uses an installed browser instead of Playwright's.
// AP_STORIES_WORKERS=n sets how many stories render at once (default: up to 4).
// Opens every story in a real browser, in English and Polish, and fails on:
//   - a missing required story (screen states of F2.5a 6c, every primitive),
//   - any request that is not served by the static Storybook itself,
//   - a page error or console error, or a play function that fails or never finishes,
//   - an axe-core accessibility violation.
const { chromium } = require('playwright')
const axeSource = require('node:fs').readFileSync(require.resolve('axe-core/axe.min.js'), 'utf8')
const http = require('node:http')
const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert/strict')
const os = require('node:os')

const root = path.resolve(__dirname, '..', 'storybook-static')
const types = { '.html': 'text/html', '.js': 'text/javascript', '.mjs': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.woff2': 'font/woff2', '.svg': 'image/svg+xml', '.png': 'image/png', '.map': 'application/json' }

const REQUIRED = [
  'ui-primitives--pills', 'ui-primitives--buttons', 'ui-primitives--card-with-header', 'ui-primitives--segmented', 'ui-primitives--kpis', 'ui-primitives--fields',
  'ui-data--outcomes', 'ui-data--events', 'ui-data--timeline', 'ui-data--yaml',
  'ui-shell--sidebar-with-deferred-entries', 'ui-shell--sidebar-recent-runs', 'ui-shell--sidebar-recent-unavailable', 'ui-shell--sidebar-no-runs', 'ui-shell--rail', 'ui-shell--global-bar-connected', 'ui-shell--global-bar-lost', 'ui-shell--breadcrumbs',
  'screens--runs-list-empty', 'screens--runs-list-busy', 'screens--inbox-empty', 'screens--inbox-no-runs', 'screens--inbox-busy', 'screens--inbox-needs-attention', 'screens--inbox-failed-selected', 'screens--inbox-retries-used-up', 'screens--inbox-interrupted-selected', 'screens--inbox-running-selected', 'screens--inbox-filter-stopped', 'screens--inbox-filter-empty', 'screens--inbox-decided', 'screens--inbox-decided-here', 'screens--inbox-run-not-found', 'screens--inbox-stays-on-shown-item', 'screens--inbox-keyboard-focus', 'screens--inbox-decision-error', 'screens--inbox-connection-lost',
  'screens--inspector-running', 'screens--inspector-waiting-for-human', 'screens--inspector-failed-after-retries', 'screens--inspector-succeeded', 'screens--inspector-yaml-view', 'screens--inspector-split-view', 'screens--inspector-first-attempt', 'screens--inspector-compare-shell-attempts', 'screens--inspector-compare-agent-scope', 'screens--inspector-compare-scope-indeterminate','screens--inspector-connection-lost', 'screens--inspector-run-not-found',
  'screens--approvals-empty', 'screens--approvals-one-pending', 'screens--approvals-many-pending', 'screens--approvals-decision-error',
  'screens--settings-language',
  'screens-editor--empty', 'screens-editor--empty-no-recent', 'screens-editor--opened', 'screens-editor--opened-config-error', 'screens-editor--opened-parse-error', 'screens-editor--unsaved', 'screens-editor--yaml-error-while-typing', 'screens-editor--new-graph', 'screens-editor--save-conflict', 'screens-editor--file-gone', 'screens-editor--save-failed', 'screens-editor--leave-prompt', 'screens-editor--open-failed',
  'screens-editor--canvas-graph', 'screens-editor--canvas-node-selected', 'screens-editor--canvas-connect-list', 'screens-editor--canvas-rename', 'screens-editor--canvas-delete-confirm', 'screens-editor--canvas-add-node', 'screens-editor--canvas-operation-refused', 'screens-editor--canvas-read-only-anchors', 'screens-editor--canvas-yaml-broken', 'screens-editor--canvas-empty-graph',
  'screens-editor--bindings-rows', 'screens-editor--bindings-with-issues', 'screens-editor--bindings-empty', 'screens-editor--bindings-add-form', 'screens-editor--bindings-replace-confirm', 'screens-editor--bindings-read-only', 'screens-editor--bindings-conflict',
  'screens-editor--graph-inputs-card', 'screens-editor--graph-input-removal-preview', 'screens-editor--graph-outputs-card', 'screens-editor--hint-unused-output-options', 'screens-editor--hint-unbound-input-options',
  'screens-editor--run-inputs-required', 'screens-editor--run-blocked-by-errors', 'screens-editor--run-failed-to-start', 'screens-editor--run-after-save-conflict', 'screens-editor--opening-graph',
  'screens-editor--issues-none', 'screens-editor--issues-warnings-only', 'screens-editor--issues-errors-and-warnings', 'screens-editor--issues-filtered', 'screens-editor--issues-focused-edge', 'screens-editor--issues-focused-node', 'screens-editor--issues-focused-port', 'screens-editor--issues-parse-error', 'screens-editor--issues-parse-error-column', 'screens-editor--issues-validating', 'screens-editor--issues-engine-unreachable',
  'screens-inspector--nothing-selected', 'screens-inspector--shell-with-json-predicate-loop', 'screens-inspector--advanced-section', 'screens-inspector--loop-command-gate', 'screens-inspector--loop-json-predicate-gate', 'screens-inspector--loop-reported-file-scope-gate', 'screens-inspector--agent-loop-changed-files-scope', 'screens-inspector--agent-task-from-input', 'screens-inspector--standalone-gate', 'screens-inspector--human-node', 'screens-inspector--unknown-node-type', 'screens-inspector--field-with-diagnostic', 'screens-inspector--loop-needs-an-output', 'screens-inspector--port-removal-preview', 'screens-inspector--read-only-while-yaml-is-broken', 'screens-inspector--checking', 'screens-inspector--engine-unreachable',
  'screens-inspector--loop-edits-are-targeted', 'screens-inspector--refused-rename-restores-name', 'screens-inspector--cancelled-rename-restores-name', 'screens-inspector--invalid-max-attempts', 'screens-inspector--map-key-collision', 'screens-inspector--gate-input-and-port-diagnostics', 'screens-inspector--output-rename-carries-type',
  'screens--runs-list-loading', 'screens--runs-list-error', 'screens--inbox-loading', 'screens--inbox-error', 'screens--approvals-loading', 'screens--approvals-error', 'screens--full-app-shell-engine-down',
  'screens-new-run--no-graph', 'screens-new-run--recent-graphs', 'screens-new-run--agent-graph-ready', 'screens-new-run--agent-graph-codex-not-configured', 'screens-new-run--agent-graph-checking-codex', 'screens-new-run--ready', 'screens-new-run--input-required', 'screens-new-run--input-filled', 'screens-new-run--warnings', 'screens-new-run--blocked', 'screens-new-run--validating', 'screens-new-run--engine-unreachable', 'screens-new-run--start-fails', 'screens-new-run--pick-fails',
  'screens-welcome--all-available', 'screens-welcome--git-missing', 'screens-welcome--codex-not-configured', 'screens-welcome--codex-found-on-path', 'screens-welcome--agent-clis-found', 'screens-welcome--codex-unsupported-version', 'screens-welcome--windows', 'screens-welcome--detection-error', 'screens-welcome--checking', 'screens-welcome--rechecking', 'screens-welcome--request-failed', 'screens-welcome--engine-disconnected',
  'screens-edge-inspector--no-run', 'screens-edge-inspector--run-unavailable', 'screens-edge-inspector--different-version-none-chosen', 'screens-edge-inspector--different-version-needs-confirmation', 'screens-edge-inspector--different-version-confirmed', 'screens-edge-inspector--node-not-finished', 'screens-edge-inspector--node-waiting-for-person', 'screens-edge-inspector--node-failed', 'screens-edge-inspector--older-run', 'screens-edge-inspector--not-recorded', 'screens-edge-inspector--no-value-for-this-port', 'screens-edge-inspector--value-not-carried', 'screens-edge-inspector--value-string', 'screens-edge-inspector--value-json-formatted', 'screens-edge-inspector--value-json-raw', 'screens-edge-inspector--value-not-json', 'screens-edge-inspector--value-truncated', 'screens-edge-inspector--value-redacted', 'screens-edge-inspector--value-long', 'screens-edge-inspector--value-hidden-characters', 'screens-edge-inspector--value-empty', 'screens-edge-inspector--value-artifact-reference', 'screens-edge-inspector--live-run-value-arrived', 'screens-edge-inspector--connection-lost', 'screens-edge-inspector--connection-lost-before-value',
  'screens-template-picker--default', 'screens-template-picker--windows-report', 'screens-template-picker--no-report', 'screens-template-picker--codex-not-configured', 'screens-template-picker--agent-draft-shows-placeholder',
]

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1')
  if (url.pathname === '/favicon.ico') return res.writeHead(204).end() // some browsers always ask
  let file = path.join(root, decodeURIComponent(url.pathname))
  if (!file.startsWith(root)) return res.writeHead(403).end()
  if (fs.existsSync(file) && fs.statSync(file).isDirectory()) file = path.join(file, 'index.html')
  if (!fs.existsSync(file)) return res.writeHead(404).end()
  res.writeHead(200, { 'Content-Type': types[path.extname(file)] ?? 'application/octet-stream' }).end(fs.readFileSync(file))
})

;(async () => {
  assert.ok(fs.existsSync(path.join(root, 'index.json')), 'run npm run build-storybook first')
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  const origin = `http://127.0.0.1:${server.address().port}`
  const index = JSON.parse(fs.readFileSync(path.join(root, 'index.json'), 'utf8'))
  const ids = Object.values(index.entries).filter((e) => e.type === 'story').map((e) => e.id)
  const missing = REQUIRED.filter((id) => !ids.includes(id))
  assert.deepEqual(missing, [], `missing stories: ${missing.join(', ')}`)

  const browser = await chromium.launch(process.env.AP_CHROMIUM ? { executablePath: process.env.AP_CHROMIUM, args: ['--no-sandbox'] } : {}).catch((e) => {
    throw new Error(`${e.message}\nInstall the browser once with: npx playwright install chromium`)
  })
  const failures = []
  const workers = Math.max(1, Number(process.env.AP_STORIES_WORKERS) || Math.min(4, os.cpus().length))
  for (const locale of ['en', 'pl']) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    const queue = [...ids]
    // Each page takes the next story from the shared queue; failures name the
    // story that page was rendering, so the report matches a sequential run.
    const worker = async () => {
      const page = await context.newPage()
      let current = ''
      const fail = (msg) => failures.push(`[${locale}] ${current}: ${msg}`)
      page.on('request', (r) => {
        if (!r.url().startsWith(origin) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) fail(`external request ${r.url()}`)
      })
      // Storybook ends a story whose play function threw as "finished" and reports the error on its
      // channel; record it from the first moment the channel exists.
      await page.addInitScript(() => {
        const hook = setInterval(() => {
          const channel = window.__STORYBOOK_ADDONS_CHANNEL__
          if (!channel) return
          clearInterval(hook)
          channel.on('playFunctionThrewException', (e) => {
            window.__apPlayError = String((e && e.message) || e)
          })
        }, 5)
      })
      page.on('pageerror', (e) => fail(`page error ${e.message}`))
      page.on('console', (m) => m.type() === 'error' && fail(`console error ${m.text()}`))
      while (queue.length) {
        current = queue.shift()
        await checkStory(page, origin, current, locale, fail)
      }
      await page.close()
    }
    await Promise.all(Array.from({ length: workers }, worker))
    await context.close()
  }
  await browser.close()
  server.close()
  if (failures.length) {
    console.error(failures.join('\n'))
    process.exit(1)
  }
  console.log(`stories ok: ${ids.length} stories x 2 languages, zero axe violations, zero external requests`)
})().catch((e) => {
  console.error(e)
  process.exit(1)
})

async function checkStory(page, origin, id, locale, fail) {
  await page.goto(`${origin}/iframe.html?id=${id}&viewMode=story&globals=locale:${locale}`)
  await page.waitForSelector('#storybook-root > *', { timeout: 15000 }).catch(() => fail('story did not render'))
  // Wait for the story to finish, play function included, so a failing play fails the smoke and
  // axe checks the state the play function reached, not the first render.
  const phase = await page
    .waitForFunction(() => {
      const p = window.__STORYBOOK_PREVIEW__?.currentRender?.phase
      return ['finished', 'errored', 'aborted'].includes(p) && p
    }, null, { timeout: 15000, polling: 50 })
    .then((h) => h.jsonValue())
    .catch(() => 'timeout')
  if (phase !== 'finished') return fail(`story did not finish (${phase}): its play function failed or never ended`)
  const playError = await page.evaluate(() => window.__apPlayError)
  if (playError) return fail(`play function failed: ${playError}`)
  // Only React Flow views fade in after fit-view; other stories are ready once rendered.
  if (await page.$('#storybook-root .react-flow')) await page.waitForTimeout(900)
  await page.addScriptTag({ content: axeSource })
  // The Storybook a11y addon runs its own axe on render; wait for it to finish.
  let result
  for (let tries = 0; tries < 20 && !result; tries++) {
    try {
      result = await page.evaluate(() => axe.run('#storybook-root'))
    } catch (e) {
      if (!/already running/.test(String(e))) throw e
      await page.waitForTimeout(250)
    }
  }
  if (!result) return fail('axe never became available')
  for (const v of result.violations) fail(`axe ${v.id}: ${v.help} (${v.nodes.length} nodes) e.g. ${v.nodes[0].html.slice(0, 160)} -> ${(v.nodes[0].any[0] || v.nodes[0].all[0] || {}).message}`)
}
