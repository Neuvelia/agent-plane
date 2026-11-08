// Runs with Electron, never bundled into the application.
// Pass --force-device-scale-factor=1 before this script: Chromium initializes
// display scaling before app.commandLine switches are applied on macOS Retina.
const { app, BrowserWindow } = require('electron')
const fs = require('node:fs/promises')
const path = require('node:path')
app.commandLine.appendSwitch('force-prefers-reduced-motion')
app.whenReady().then(async () => {
  const output = process.argv.at(-1)
  if (!output) throw new Error('Pass output directory')
  await fs.mkdir(output, { recursive: true })
  const window = new BrowserWindow({ width: 1080, height: 720, useContentSize: true, show: false,
    backgroundColor: '#0a0b0b', webPreferences: { preload: path.join(__dirname, 'visual-preload.cjs'), contextIsolation: true, nodeIntegration: false, sandbox: true } })
  await window.loadFile(path.join(__dirname, '..', 'out', 'renderer', 'index.html'))
  window.webContents.setZoomFactor(1)
  const metrics = await window.webContents.executeJavaScript(`(async () => { await document.fonts.ready; await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); return { width: innerWidth, height: innerHeight, dpr: devicePixelRatio, font: document.fonts.status, reducedMotion: matchMedia('(prefers-reduced-motion: reduce)').matches } })()`)
  if (metrics.width !== 1080 || metrics.height !== 720 || metrics.dpr !== 1 || metrics.font !== 'loaded' || !metrics.reducedMotion || window.webContents.getZoomFactor() !== 1) throw new Error(`Invalid capture metrics: ${JSON.stringify(metrics)}`)
  for (const [page, label] of [['dashboard', 'Dashboard'], ['project', 'Projects'], ['runner', 'This device'], ['settings', 'Settings']]) {
    if (page !== 'dashboard') await window.webContents.executeJavaScript(`document.querySelector('button[aria-label=${JSON.stringify(label)}]').click()`)
    await window.webContents.executeJavaScript(`(async () => { await document.fonts.ready; await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); })()`)
    if (page === 'dashboard' || page === 'project') await window.webContents.executeJavaScript(`new Promise((resolve, reject) => {
      if (document.body.innerText.includes('No local run history')) return resolve(true)
      const observer = new MutationObserver(() => { if (document.body.innerText.includes('No local run history')) { observer.disconnect(); clearTimeout(timer); resolve(true) } })
      observer.observe(document.body, { childList: true, subtree: true, characterData: true })
      const timer = setTimeout(() => { observer.disconnect(); reject(new Error('Empty history state did not render')) }, 3000)
    })`)
    const body = await window.webContents.executeJavaScript('document.body.innerText')
    const expected = { dashboard: 'Recent history', project: 'Workspaces & targets', runner: 'CAPABILITIES', settings: 'Effective configuration' }[page]
    if (!body.includes(expected)) throw new Error(`Missing ${page} fixture content`)
    if ((page === 'dashboard' || page === 'project') && !body.includes('No local run history')) throw new Error(`Missing ${page} empty history state`)
    if (page === 'settings' && !body.includes('Applied when pruning')) throw new Error('Settings retention does not describe current behavior')
    if (page === 'dashboard') {
      const disabled = await window.webContents.executeJavaScript(`[...document.querySelectorAll('button')].filter(b => b.textContent.includes('Run graph')).every(b => b.disabled)`)
      if (!disabled) throw new Error('Unsupported Run graph action is enabled')
    }
    await fs.writeFile(path.join(output, `${page}.png`), await window.webContents.capturePage().then((image) => image.toPNG()))
  }
  const graphPrompt = await window.webContents.executeJavaScript(`(async () => {
    document.querySelector('button[aria-label=Projects]').click()
    await new Promise(r => requestAnimationFrame(r))
    const prompt = document.querySelector('button.graph-open-prompt')
    const before = { focusable: prompt?.tabIndex >= 0, expanded: prompt?.getAttribute('aria-expanded'), input: !!document.querySelector('input[aria-label="Graph path relative to project"]') }
    prompt?.click()
    await new Promise(r => requestAnimationFrame(r))
    const afterClick = { expanded: prompt?.getAttribute('aria-expanded'), input: !!document.querySelector('input[aria-label="Graph path relative to project"]'), open: !![...document.querySelectorAll('button')].find(b => b.textContent === 'Open') }
    prompt?.click()
    await new Promise(r => requestAnimationFrame(r))
    prompt?.focus()
    return { before, afterClick, focused: document.activeElement === prompt }
  })()`)
  if (!graphPrompt.before.focusable || graphPrompt.before.expanded !== 'false' || graphPrompt.before.input || graphPrompt.afterClick.expanded !== 'true' || !graphPrompt.afterClick.input || !graphPrompt.afterClick.open || !graphPrompt.focused) throw new Error(`Project graph disclosure failed: ${JSON.stringify(graphPrompt)}`)
  window.show()
  window.focus()
  window.webContents.focus()
  const graphRefocused = await window.webContents.executeJavaScript(`(() => { const prompt = document.querySelector('button.graph-open-prompt'); prompt?.focus(); return document.activeElement === prompt })()`)
  if (!graphRefocused) throw new Error('Project graph disclosure lost focus before keyboard activation')
  window.webContents.debugger.attach('1.3')
  try {
    await window.webContents.debugger.sendCommand('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', text: '\r', unmodifiedText: '\r', windowsVirtualKeyCode: 13, nativeVirtualKeyCode: 13 })
    await window.webContents.debugger.sendCommand('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13, nativeVirtualKeyCode: 13 })
  } finally {
    window.webContents.debugger.detach()
  }
  const graphKeyboard = await window.webContents.executeJavaScript(`(async () => { await new Promise(r => requestAnimationFrame(r)); return { expanded: document.querySelector('button.graph-open-prompt')?.getAttribute('aria-expanded'), input: !!document.querySelector('input[aria-label="Graph path relative to project"]'), open: !![...document.querySelectorAll('button')].find(b => b.textContent === 'Open'), focus: document.activeElement?.className } })()`)
  if (graphKeyboard.expanded !== 'true' || !graphKeyboard.input || !graphKeyboard.open) throw new Error(`Project graph keyboard disclosure failed: ${JSON.stringify(graphKeyboard)}`)
  const keyboard = await window.webContents.executeJavaScript(`(async () => { const b = document.querySelector('button[aria-label=Dashboard]'); b.focus(); b.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })); await new Promise(r => requestAnimationFrame(r)); return { focused: document.activeElement?.getAttribute('aria-label'), selected: document.querySelector('button[aria-current=page]')?.getAttribute('aria-label') } })()`)
  if (keyboard.focused !== 'Projects' || keyboard.selected !== 'Projects') throw new Error(`Keyboard navigation failed: ${JSON.stringify(keyboard)}`)
  const projectSwitch = await window.webContents.executeJavaScript(`(async () => { window.visualFixture.holdWorkspaces(); [...document.querySelectorAll('.sidebar-link')].find(e => e.textContent.includes('Thesis experiments')).click(); await new Promise(r => requestAnimationFrame(r)); return { text: document.querySelector('main.content')?.innerText, title: document.querySelector('.sidebar-title')?.innerText } })()`)
  if (projectSwitch.title !== 'Thesis experiments' || projectSwitch.text.includes('ws_acme_api') || !projectSwitch.text.includes('Loading workspaces')) throw new Error('Project switch leaked old workspace state')
  await window.webContents.executeJavaScript(`window.visualFixture.releaseWorkspaces()`)
  const missing = await window.webContents.executeJavaScript(`(async () => { window.visualFixture.markSecondProjectMissing(); [...document.querySelectorAll('button')].find(b => b.textContent === 'Refresh').click(); await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); return { text: document.querySelector('main.content')?.innerText, title: document.querySelector('.sidebar-title')?.innerText } })()`)
  if (missing.title !== 'Thesis experiments' || !missing.text.includes('missing')) throw new Error('Refresh did not preserve route and update missing availability')
  const failedRefresh = await window.webContents.executeJavaScript(`(async () => { [...document.querySelectorAll('.sidebar-link')].find(e => e.textContent.includes('Backend API')).click(); await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); window.visualFixture.failProjectsOnce(); [...document.querySelectorAll('button')].find(b => b.textContent === 'Refresh').click(); await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); return { text: document.querySelector('main.content')?.innerText } })()`)
  if (!failedRefresh.text.includes('ws_acme_api') || !failedRefresh.text.includes('stale cursor')) throw new Error('Failed Refresh discarded last safe workspace data')
  const removed = await window.webContents.executeJavaScript(`(async () => { [...document.querySelectorAll('.sidebar-link')].find(e => e.textContent.includes('Backend API')).click(); await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); window.visualFixture.removeWorkspace(); [...document.querySelectorAll('button')].find(b => b.textContent === 'Refresh').click(); await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); return { text: document.querySelector('main.content')?.innerText } })()`)
  if (removed.text.includes('ws_acme_api') || !removed.text.includes('No registered workspaces')) throw new Error('Refresh did not remove deleted workspace')
  await window.webContents.executeJavaScript(`document.querySelector('button[aria-label=Settings]').click()`)
  const conflict = await window.webContents.executeJavaScript(`(async () => { const input = document.querySelector('input[type=number]'); input.focus(); input.value = '3'; input.blur(); input.dispatchEvent(new FocusEvent('focusout', { bubbles: true })); await new Promise(r => setTimeout(r, 0)); await new Promise(r => requestAnimationFrame(r)); return { value: input.value, active: document.activeElement?.tagName, error: document.querySelector('[role=alert]')?.innerText, reload: !![...document.querySelectorAll('button')].find(b => b.textContent === 'Reload local data') } })()`)
  if (!conflict.error?.includes('revision conflict') || !conflict.reload) throw new Error(`Settings conflict recovery is not visible: ${JSON.stringify(conflict)}`)
  const disconnect = await window.webContents.executeJavaScript(`(async () => { window.visualFixture.holdProjects(); [...document.querySelectorAll('button')].find(b => b.textContent === 'Reload local data').click(); window.visualFixture.disconnect(); window.visualFixture.releaseProjects(); await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); return { text: document.body.innerText, disabled: [...document.querySelectorAll('.preferences input')].every(e => e.disabled) } })()`)
  if (!disconnect.text.includes('Local service unavailable') || !disconnect.text.includes('Stale') || !disconnect.disabled) throw new Error('Disconnect did not keep stale read-only state')
  console.log(JSON.stringify({ ...metrics, graphPrompt: 'pass', graphKeyboard: 'pass', keyboard, projectSwitch: 'pass', missingAvailability: 'pass', failedRefresh: 'pass', deletedWorkspace: 'pass', conflict: 'pass', disconnect: 'pass' }))
  const compact = new BrowserWindow({ width: 900, height: 620, useContentSize: true, show: false, backgroundColor: '#0a0b0b', webPreferences: { preload: path.join(__dirname, 'visual-preload.cjs'), contextIsolation: true, nodeIntegration: false, sandbox: true } })
  await compact.loadFile(path.join(__dirname, '..', 'out', 'renderer', 'index.html'))
  const responsive = await compact.webContents.executeJavaScript(`(async () => { await document.fonts.ready; await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); return { width: innerWidth, height: innerHeight, scrollWidth: document.documentElement.scrollWidth, rail: !!document.querySelector('.rail'), sidebar: !!document.querySelector('.sidebar') } })()`)
  if (responsive.width !== 900 || responsive.height !== 620 || responsive.scrollWidth > 900 || !responsive.rail || !responsive.sidebar) throw new Error(`Minimum viewport failed: ${JSON.stringify(responsive)}`)
  await fs.writeFile(path.join(output, 'responsive-900x620.png'), await compact.webContents.capturePage().then((image) => image.toPNG()))
  compact.destroy()
  window.destroy()
  app.quit()
}).catch((error) => { console.error(error); app.exit(1) })
