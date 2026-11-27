'use strict'
// node scripts/screenshots.cjs [outDir]   (after `npm run build-storybook`)
// Writes PNGs of the key screens in English and Polish from the real components
// over recorded event fixtures: docs/validation/F2.5a-screenshots/{en,pl}/NAME.png
const { chromium } = require('playwright')
const http = require('node:http')
const fs = require('node:fs')
const path = require('node:path')

const root = path.resolve(__dirname, '..', 'storybook-static')
const out = path.resolve(process.argv[2] ?? path.join(__dirname, '..', '..', 'docs', 'validation', 'F2.5a-screenshots'))
const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.woff2': 'font/woff2', '.png': 'image/png', '.svg': 'image/svg+xml' }
const SHOTS = {
  'inbox': 'screens--full-app-shell',
  'inbox-needs-attention': 'screens--inbox-needs-attention',
  'inspector-running': 'screens--inspector-running',
  'inspector-waiting-for-human': 'screens--inspector-waiting-for-human',
  'inspector-failed-after-retries': 'screens--inspector-failed-after-retries',
  'inspector-connection-lost': 'screens--inspector-connection-lost',
  'approvals': 'screens--approvals-one-pending',
  'new-run-ready': 'screens--new-run-ready',
  'new-run-blocked': 'screens--new-run-blocked',
  'settings': 'screens--settings-language',
  'engine-down': 'screens--full-app-shell-engine-down',
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1')
  if (url.pathname === '/favicon.ico') return res.writeHead(204).end()
  const file = path.join(root, decodeURIComponent(url.pathname))
  if (!file.startsWith(root) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) return res.writeHead(404).end()
  res.writeHead(200, { 'Content-Type': types[path.extname(file)] ?? 'application/octet-stream' }).end(fs.readFileSync(file))
})

;(async () => {
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  const origin = `http://127.0.0.1:${server.address().port}`
  const browser = await chromium.launch(process.env.AP_CHROMIUM ? { executablePath: process.env.AP_CHROMIUM, args: ['--no-sandbox'] } : {})
  for (const locale of ['en', 'pl']) {
    fs.mkdirSync(path.join(out, locale), { recursive: true })
    const page = await (await browser.newContext({ viewport: { width: 1280, height: 800 } })).newPage()
    for (const [name, id] of Object.entries(SHOTS)) {
      await page.goto(`${origin}/iframe.html?id=${id}&viewMode=story&globals=locale:${locale}`)
      await page.waitForSelector('#storybook-root > *')
      await page.waitForTimeout(1500)
      await page.screenshot({ path: path.join(out, locale, `${name}.png`) })
    }
  }
  await browser.close()
  server.close()
  console.log(`screenshots written to ${out}`)
})().catch((e) => {
  console.error(e)
  process.exit(1)
})
