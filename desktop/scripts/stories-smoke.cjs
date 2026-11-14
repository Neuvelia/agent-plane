'use strict'
// node scripts/stories-smoke.cjs   (after `npm run build-storybook`)
// AP_CHROMIUM=/path/to/chrome uses an installed browser instead of Playwright's.
// Opens every story in a real browser, in English and Polish, and fails on:
//   - a missing required story (screen states of F2.5a 6c, every primitive),
//   - any request that is not served by the static Storybook itself,
//   - a page error or console error,
//   - an axe-core accessibility violation.
const { chromium } = require('playwright')
const axeSource = require('node:fs').readFileSync(require.resolve('axe-core/axe.min.js'), 'utf8')
const http = require('node:http')
const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert/strict')

const root = path.resolve(__dirname, '..', 'storybook-static')
const types = { '.html': 'text/html', '.js': 'text/javascript', '.mjs': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.woff2': 'font/woff2', '.svg': 'image/svg+xml', '.png': 'image/png', '.map': 'application/json' }

const REQUIRED = [
  'ui-primitives--pills', 'ui-primitives--buttons', 'ui-primitives--card-with-header', 'ui-primitives--segmented', 'ui-primitives--kpis', 'ui-primitives--fields',
  'ui-data--outcomes', 'ui-data--events', 'ui-data--timeline', 'ui-data--yaml',
  'ui-shell--sidebar-with-deferred-entries', 'ui-shell--rail', 'ui-shell--global-bar-connected', 'ui-shell--global-bar-lost', 'ui-shell--breadcrumbs',
  'screens--runs-list-empty', 'screens--runs-list-busy', 'screens--dashboard-empty', 'screens--dashboard-busy', 'screens--dashboard-needs-attention',
  'screens--inspector-running', 'screens--inspector-waiting-for-human', 'screens--inspector-failed-after-retries', 'screens--inspector-succeeded', 'screens--inspector-yaml-view', 'screens--inspector-split-view', 'screens--inspector-first-attempt', 'screens--inspector-connection-lost', 'screens--inspector-run-not-found',
  'screens--approvals-empty', 'screens--approvals-one-pending', 'screens--approvals-many-pending', 'screens--approvals-decision-error',
  'screens--settings-language',
  'screens-editor--empty', 'screens-editor--empty-no-recent', 'screens-editor--opened', 'screens-editor--opened-config-error', 'screens-editor--opened-parse-error', 'screens-editor--unsaved', 'screens-editor--yaml-error-while-typing', 'screens-editor--new-graph', 'screens-editor--save-conflict', 'screens-editor--file-gone', 'screens-editor--save-failed', 'screens-editor--leave-prompt', 'screens-editor--open-failed',
  'screens--runs-list-loading', 'screens--runs-list-error', 'screens--dashboard-loading', 'screens--dashboard-error', 'screens--approvals-loading', 'screens--approvals-error', 'screens--new-run-validating', 'screens--full-app-shell-engine-down',
  'screens--new-run-no-file', 'screens--new-run-ready', 'screens--new-run-warnings', 'screens--new-run-blocked',
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
  for (const locale of ['en', 'pl']) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    const page = await context.newPage()
    let current = ''
    const fail = (msg) => failures.push(`[${locale}] ${current}: ${msg}`)
    page.on('request', (r) => {
      if (!r.url().startsWith(origin) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) fail(`external request ${r.url()}`)
    })
    page.on('pageerror', (e) => fail(`page error ${e.message}`))
    page.on('console', (m) => m.type() === 'error' && fail(`console error ${m.text()}`))
    for (const id of ids) {
      current = id
      await page.goto(`${origin}/iframe.html?id=${id}&viewMode=story&globals=locale:${locale}`)
      await page.waitForSelector('#storybook-root > *', { timeout: 15000 }).catch(() => fail('story did not render'))
      await page.waitForTimeout(900) // let React Flow finish its fit-view fade-in
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
      if (!result) { fail('axe never became available'); continue }
      for (const v of result.violations) fail(`axe ${v.id}: ${v.help} (${v.nodes.length} nodes) e.g. ${v.nodes[0].html.slice(0, 160)} -> ${(v.nodes[0].any[0] || v.nodes[0].all[0] || {}).message}`)
    }
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
