'use strict'
// node scripts/stories-smoke.cjs   (after `npm run build-storybook`)
// AP_CHROMIUM=/path/to/chrome uses an installed browser instead of Playwright's.
// AP_STORIES_WORKERS=n sets how many stories render at once (default: up to 4).
// Opens every story in a real browser, in English and Polish and in the dark
// and light themes (F2.5b), and fails on:
//   - a missing required story (screen states of F2.5a 6c, every primitive),
//   - any request that is not served by the static Storybook itself,
//   - a page error or console error,
//   - an axe-core accessibility violation,
//   - a broken profile menu interaction (keyboard, focus, theme switch, motion).
// AP_STORIES_THEMES=dark,light limits the themes (default: both).
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
  'ui-shell--sidebar-with-deferred-entries', 'ui-shell--rail', 'ui-shell--global-bar-connected', 'ui-shell--global-bar-lost', 'ui-shell--breadcrumbs',
  'ui-shell--profile-menu-open', 'ui-shell--profile-menu-language', 'ui-shell--profile-menu-appearance', 'ui-shell--profile-menu-appearance-light', 'ui-shell--profile-menu-open-light',
  'ui-shell--keyboard-shortcuts', 'ui-shell--about-tarmind', 'ui-shell--about-engine-unreachable', 'ui-shell--about-tarmind-light',
  'screens--runs-list-empty', 'screens--runs-list-busy', 'screens--dashboard-empty', 'screens--dashboard-busy', 'screens--dashboard-needs-attention',
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
  'screens--runs-list-loading', 'screens--runs-list-error', 'screens--dashboard-loading', 'screens--dashboard-error', 'screens--approvals-loading', 'screens--approvals-error', 'screens--new-run-validating', 'screens--full-app-shell-engine-down',
  'screens--new-run-no-file', 'screens--new-run-ready', 'screens--new-run-warnings', 'screens--new-run-blocked',
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
  const themes = (process.env.AP_STORIES_THEMES || 'dark,light').split(',')
  for (const [locale, theme] of ['en', 'pl'].flatMap((l) => themes.map((t) => [l, t]))) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    const queue = [...ids]
    // Each page takes the next story from the shared queue; failures name the
    // story that page was rendering, so the report matches a sequential run.
    const worker = async () => {
      const page = await context.newPage()
      let current = ''
      const fail = (msg) => failures.push(`[${locale}/${theme}] ${current}: ${msg}`)
      page.on('request', (r) => {
        if (!r.url().startsWith(origin) && !r.url().startsWith('data:') && !r.url().startsWith('blob:')) fail(`external request ${r.url()}`)
      })
      page.on('pageerror', (e) => fail(`page error ${e.message}`))
      page.on('console', (m) => m.type() === 'error' && fail(`console error ${m.text()}`))
      while (queue.length) {
        current = queue.shift()
        await checkStory(page, origin, current, `locale:${locale};theme:${theme}`, fail)
      }
      await page.close()
    }
    await Promise.all(Array.from({ length: workers }, worker))
    await context.close()
  }
  await checkProfileMenu(browser, origin, (msg) => failures.push(`[profile menu] ${msg}`))
  await browser.close()
  server.close()
  if (failures.length) {
    console.error(failures.join('\n'))
    process.exit(1)
  }
  console.log(`stories ok: ${ids.length} stories x 2 languages x ${themes.length} themes, zero axe violations, zero external requests, profile menu interactions ok`)
})().catch((e) => {
  console.error(e)
  process.exit(1)
})

async function checkStory(page, origin, id, globals, fail) {
  await page.goto(`${origin}/iframe.html?id=${id}&viewMode=story&globals=${globals}`)
  await page.waitForSelector('#storybook-root > *', { timeout: 15000 }).catch(() => fail('story did not render'))
  // Only React Flow views fade in after fit-view; other stories are ready once rendered.
  if (await page.$('#storybook-root .react-flow')) await page.waitForTimeout(900)
  await settleAnimations(page)
  const painted = await page.evaluate(() => document.documentElement.dataset.theme)
  const wanted = /theme:light/.test(globals) || /-light$/.test(id) ? 'light' : 'dark'
  if (painted !== wanted) fail(`theme ${painted} painted, ${wanted} expected`)
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

/** Waits for finite animations (menu and dialog entrances) so axe measures final colours, not a fade. */
async function settleAnimations(page) {
  await page.evaluate(() => Promise.all(document.getAnimations().filter((a) => a.effect?.getComputedTiming().iterations !== Infinity).map((a) => a.finished.catch(() => undefined))))
}

// roadmap: F2.5b-AC5
// roadmap: F2.5b-AC6
// The profile menu in a real browser: keyboard model, focus return, the
// submenu's alignment, the theme switch and motion (on, and off under reduced motion).
async function checkProfileMenu(browser, origin, fail) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page = await context.newPage()
  page.on('pageerror', (e) => fail(`page error ${e.message}`))
  const expect = (cond, msg) => cond || fail(msg)
  const focused = () => page.evaluate(() => document.activeElement?.getAttribute('data-testid') ?? document.activeElement?.tagName)
  const theme = () => page.evaluate(() => document.documentElement.dataset.theme)
  const menuOpen = async () => (await page.$('[data-testid="profile-menu"]:not(.closing)')) !== null
  const level = (sub) => page.evaluate((s) => [document.querySelector(`[data-sub="${s}"]`).getBoundingClientRect().bottom, [...document.querySelectorAll('.pm-sub .mi')].pop().getBoundingClientRect().bottom], sub)
  // A story that starts with a submenu open places it on first mount, too.
  await page.goto(`${origin}/iframe.html?id=ui-shell--profile-menu-language&viewMode=story&globals=locale:en;theme:dark`)
  await page.waitForSelector('[data-testid="profile-submenu"]')
  await settleAnimations(page)
  const [lt, ll] = await level('language')
  expect(Math.abs(lt - ll) <= 1, `on first mount the submenu's last item (${ll}) is not level with its trigger (${lt})`)
  await page.goto(`${origin}/iframe.html?id=ui-shell--sidebar-with-deferred-entries&viewMode=story&globals=locale:en;theme:dark`)
  await page.waitForSelector('[data-testid="profile"]')

  // Open by click: the menu animates in and focus lands on its first item.
  await page.click('[data-testid="profile"]')
  await page.waitForSelector('[data-testid="profile-menu"]')
  expect((await page.evaluate(() => getComputedStyle(document.querySelector('.pm')).animationName)) === 'pm-in', 'the menu has no entrance animation')
  expect((await focused()) === 'profile-settings', `first item not focused on open: ${await focused()}`)
  expect((await page.getAttribute('[data-testid="profile"]', 'aria-expanded')) === 'true', 'button not expanded')
  // Arrows rove and wrap; Home and End jump.
  await page.keyboard.press('ArrowUp')
  expect((await focused()) === 'profile-about', `ArrowUp from the first item should wrap to About: ${await focused()}`)
  await page.keyboard.press('Home')
  expect((await focused()) === 'profile-settings', 'Home did not reach Settings')
  for (let i = 0; i < 3; i++) await page.keyboard.press('ArrowDown')
  expect((await focused()) === 'profile-appearance', `three ArrowDowns should reach Appearance: ${await focused()}`)
  // ArrowRight opens the submenu on the checked choice; it lines up with its trigger.
  await page.keyboard.press('ArrowRight')
  await page.waitForSelector('[data-testid="profile-submenu"]')
  await settleAnimations(page)
  expect((await focused()) === 'profile-theme-dark', `the checked choice should take focus: ${await focused()}`)
  const [trigger, last] = await level('appearance')
  expect(Math.abs(trigger - last) <= 1, `submenu's last item (${last}) is not level with its trigger (${trigger})`)
  // Picking Light paints it at once, closes the submenu and returns focus to its trigger; the menu stays open.
  await page.keyboard.press('ArrowUp')
  await page.keyboard.press('Enter')
  expect((await theme()) === 'light', `Light was not applied: ${await theme()}`)
  await page.waitForSelector('[data-testid="profile-submenu"]', { state: 'detached' })
  expect((await focused()) === 'profile-appearance', `focus did not return to Appearance: ${await focused()}`)
  expect(await menuOpen(), 'the menu closed after picking a theme')
  // Enter opens the submenu too; ArrowLeft closes only the submenu.
  await page.keyboard.press('Enter')
  await page.waitForSelector('[data-testid="profile-submenu"]')
  await page.keyboard.press('ArrowLeft')
  await page.waitForSelector('[data-testid="profile-submenu"]', { state: 'detached' })
  expect((await focused()) === 'profile-appearance', 'ArrowLeft did not return focus to the trigger')
  // Review M1 (the story pins English, so English is checked): a submenu opened by pointer still takes the keyboard. ArrowRight and Enter on its
  // open trigger move focus to the checked choice; ArrowLeft brings it back.
  await page.click('[data-testid="profile-language"]')
  await page.waitForSelector('[data-testid="profile-submenu"]')
  expect((await focused()) === 'profile-language', `a pointer click should leave focus on the trigger: ${await focused()}`)
  await page.keyboard.press('ArrowRight')
  expect((await focused()) === 'profile-language-en', `ArrowRight on a pointer-opened submenu should focus the checked choice: ${await focused()}`)
  await page.keyboard.press('ArrowLeft')
  await page.waitForSelector('[data-testid="profile-submenu"]', { state: 'detached' })
  await page.click('[data-testid="profile-language"]')
  await page.waitForSelector('[data-testid="profile-submenu"]')
  await page.keyboard.press('Enter')
  // Wait on the outcome, not a clock: focus reaches the checked choice (or the step fails).
  await page.waitForFunction(() => document.activeElement?.getAttribute('data-testid') === 'profile-language-en', null, { timeout: 2000 }).catch(() => undefined)
  expect((await page.$('[data-testid="profile-submenu"]:not(.closing)')) !== null, 'Enter on an open trigger closed its submenu')
  expect((await focused()) === 'profile-language-en', `Enter on an open trigger should focus the checked choice: ${await focused()}`)
  await page.keyboard.press('Escape')
  await page.waitForSelector('[data-testid="profile-submenu"]', { state: 'detached' })
  await page.keyboard.press('Home')
  for (let i = 0; i < 3; i++) await page.keyboard.press('ArrowDown')
  // Escape closes the menu (with an exit animation) and returns focus to the profile button.
  await page.keyboard.press('Escape')
  expect((await page.$('.pm.closing')) !== null, 'the menu left without an exit animation')
  await page.waitForSelector('[data-testid="profile-menu"]', { state: 'detached' })
  expect((await focused()) === 'profile', `Escape did not return focus to the profile button: ${await focused()}`)
  expect((await page.getAttribute('[data-testid="profile"]', 'aria-expanded')) === 'false', 'button still expanded')
  // ArrowUp on the button opens on the last item; a press outside closes.
  await page.keyboard.press('ArrowUp')
  await page.waitForSelector('[data-testid="profile-menu"]')
  expect((await focused()) === 'profile-about', `ArrowUp on the button should focus the last item: ${await focused()}`)
  await page.mouse.click(1200, 40)
  await page.waitForSelector('[data-testid="profile-menu"]', { state: 'detached' })
  // Back to Dark by pointer; the disabled Help item does nothing.
  await page.click('[data-testid="profile"]')
  await page.click('[data-testid="profile-help"]', { force: true })
  expect(await menuOpen(), 'the disabled Help item closed the menu')
  await page.click('[data-testid="profile-appearance"]')
  await page.click('[data-testid="profile-theme-dark"]')
  expect((await theme()) === 'dark', 'Dark was not applied')
  // roadmap: F2.5b-AC8
  // In the whole app: About closes on a backdrop press and focus returns to the profile button;
  // app shortcuts do nothing while a window is open; a window closed just before a shortcut
  // never closes the next one (review m1, m2).
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.goto(`${origin}/iframe.html?id=screens--full-app-shell&viewMode=story&globals=locale:en;theme:dark`)
  await page.waitForSelector('[data-testid="profile"]')
  const openAbout = async () => {
    await page.click('[data-testid="profile"]')
    await page.click('[data-testid="profile-about"]')
    await page.waitForSelector('[data-testid="about-dialog"][open]')
    await settleAnimations(page)
  }
  await openAbout()
  const route = () => page.evaluate(() => document.querySelector('main')?.innerHTML.length)
  const before = await route()
  await page.keyboard.press('Control+,')
  await page.keyboard.press('Control+/')
  expect((await page.$('[data-testid="shortcuts-dialog"]')) === null, 'Ctrl+/ opened a second window over About')
  expect((await route()) === before, 'Ctrl+, changed the screen behind About')
  await page.mouse.click(4, 4)
  await page.waitForSelector('[data-testid="about-dialog"]', { state: 'detached' })
  expect((await focused()) === 'profile', `focus did not return to the profile button after a backdrop press: ${await focused()}`)
  await openAbout()
  await page.click('[data-testid="about-dialog-close"]')
  await page.keyboard.press('Control+/')
  // The shortcut list's entrance (160 ms) outlasts About's old exit timer (140 ms): once it has
  // settled, a late onClose from About would already have closed it.
  await page.waitForSelector('[data-testid="shortcuts-dialog"][open]')
  await settleAnimations(page)
  expect((await page.$('[data-testid="shortcuts-dialog"][open]')) !== null, 'the closing About window closed the shortcut list opened after it')
  await page.keyboard.press('Escape')
  await page.waitForSelector('[data-testid="shortcuts-dialog"]', { state: 'detached' })

  // Review N1: an app shortcut closes the menu even when focus is outside it.
  await page.goto(`${origin}/iframe.html?id=screens--full-app-shell&viewMode=story&globals=locale:en;theme:dark`)
  await page.waitForSelector('[data-testid="profile"]')
  await page.click('[data-testid="profile"]')
  await page.waitForSelector('[data-testid="profile-menu"]')
  await page.click('.pm-who')
  await page.keyboard.press('Control+/')
  await page.waitForSelector('[data-testid="profile-menu"]', { state: 'detached' })
  await page.waitForSelector('[data-testid="shortcuts-dialog"][open]')
  await page.keyboard.press('Escape')
  await page.waitForSelector('[data-testid="shortcuts-dialog"]', { state: 'detached' })

  // Review m4: the YAML editor's CodeMirror base theme follows the app theme.
  const editorClasses = async (theme) => {
    await page.goto(`${origin}/iframe.html?id=screens-editor--opened&viewMode=story&globals=locale:en;theme:${theme}`)
    await page.waitForSelector('.cm-editor')
    return page.evaluate(() => [...document.querySelector('.cm-editor').classList].sort().join(' '))
  }
  const darkEditor = await editorClasses('dark')
  const lightEditor = await editorClasses('light')
  expect(darkEditor !== lightEditor, `the YAML editor looks the same in both themes (${darkEditor})`)

  // Reduced motion: no animation, and the menu leaves at once.
  await page.goto(`${origin}/iframe.html?id=ui-shell--sidebar-with-deferred-entries&viewMode=story&globals=locale:en;theme:dark`)
  await page.waitForSelector('[data-testid="profile"]')
  await page.click('[data-testid="profile"]')
  await page.waitForSelector('[data-testid="profile-menu"]')
  await page.emulateMedia({ reducedMotion: 'reduce' })
  expect((await page.evaluate(() => getComputedStyle(document.querySelector('.pm')).animationName)) === 'none', 'the menu animates under reduced motion')
  await page.keyboard.press('Escape')
  await page.waitForSelector('[data-testid="profile-menu"]', { state: 'detached', timeout: 50 }).catch(() => fail('the menu lingered under reduced motion'))
  await context.close()
}
