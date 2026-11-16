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

// The native dialog is replaced in the main process, from this test process only: a renderer cannot reach it.
const stubOpenDialog = (app, file) => app.evaluate(({ dialog }, f) => { dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [f] }) }, file)

async function startDemo(app, page) {
  await stubOpenDialog(app, graph)
  await page.click('[data-testid="nav-new"]')
  await page.click('[data-testid="choose"]')
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

    // E1.3: graph files reach the renderer only as grants; the route allowlist is exact.
    const refused = await page.evaluate(async () => ({
      grants: await window.agentplane.request('POST', '/graphs/grants', { path: '/tmp/never.yaml', mode: 'create' }),
      revoke: await window.agentplane.request('DELETE', '/graphs/grants/abc'),
      wrongMethod: await window.agentplane.request('PUT', '/runs', {}),
      climb: await window.agentplane.request('GET', '/runs/%2e%2e'),
    }))
    for (const [name, r] of Object.entries(refused)) {
      assert.equal(r.status, 400, `${name} must be refused by the main process`)
      assert.equal(r.body.error.message, 'path not allowed', name)
    }
    const graphDir = path.join(work, 'graphs')
    fs.mkdirSync(graphDir, { recursive: true })
    const original = fs.readFileSync(graph, 'utf8')
    const editable = path.join(graphDir, 'demo.yaml')
    fs.writeFileSync(editable, original)
    const stubOpen = (p) => stubOpenDialog(app, p)
    const stubSave = (p) => app.evaluate(({ dialog }, file) => { dialog.showSaveDialog = async () => ({ canceled: false, filePath: file }) }, p)
    await stubOpen(editable)
    const opened = await page.evaluate(() => window.agentplane.openGraph())
    assert.equal(opened.ok, true)
    assert.equal(opened.label, 'demo.yaml')
    assert.equal(JSON.stringify(opened).includes(work), false, 'the renderer must not be told the path')
    const doc = await page.evaluate((grant) => window.agentplane.request('POST', '/graphs/open', { grant }), opened.grant)
    assert.equal(doc.status, 200)
    assert.equal(doc.body.text, original)
    const ops = [{ op: 'set_layout', nodes: { fast: { x: 120, y: 80 } } }, { op: 'set_name', name: 'Edited in the app' }]
    const edited = await page.evaluate(([grant, hash, o]) => window.agentplane.request('POST', '/graphs/edit', { grant, base_hash: hash, ops_version: 1, ops: o }), [opened.grant, doc.body.hash, ops])
    assert.equal(edited.status, 200)
    assert.equal(fs.readFileSync(editable, 'utf8'), original, 'edit must not write the file')
    const saved = await page.evaluate(([grant, hash, text]) => window.agentplane.request('PUT', '/graphs/save', { grant, base_hash: hash, text }), [opened.grant, doc.body.hash, edited.body.text])
    assert.equal(saved.status, 200)
    const onDisk = fs.readFileSync(editable, 'utf8')
    assert.equal(onDisk, edited.body.text)
    assert.ok(onDisk.startsWith('# Phase 2 vertical slice demo'), 'the header comment survives a save from the app')
    assert.ok(onDisk.includes('fast: {x: 120, y: 80}') && onDisk.includes('name: Edited in the app'))
    const reopened = await page.evaluate((grant) => window.agentplane.request('POST', '/graphs/open', { grant }), opened.grant)
    assert.equal(reopened.body.version, doc.body.version, 'layout and name never change the semantic version')
    assert.equal(await page.evaluate((grant) => window.agentplane.closeGraph(grant), opened.grant), true)
    const closed = await page.evaluate((grant) => window.agentplane.request('POST', '/graphs/open', { grant }), opened.grant)
    assert.equal(closed.status, 404)

    // A new graph: create-only, then it joins the recent list once it exists.
    const fresh = path.join(graphDir, 'fresh')
    await stubSave(fresh)
    const made = await page.evaluate(() => window.agentplane.newGraph())
    assert.equal(made.ok, true)
    assert.equal(made.label, 'fresh.yaml')
    assert.equal(made.exists, false)
    const text = 'schema_version: 0\nid: fresh\ninputs: {}\nnodes: {}\nedges: []\noutputs: {}\n'
    const created = await page.evaluate(([grant, t]) => window.agentplane.request('PUT', '/graphs/save', { grant, base_hash: null, text: t }), [made.grant, text])
    assert.equal(created.status, 200)
    assert.equal(fs.readFileSync(path.join(graphDir, 'fresh.yaml'), 'utf8'), text)
    assert.equal(await page.evaluate((grant) => window.agentplane.touchRecent(grant), made.grant), true)
    assert.equal(await page.evaluate(() => window.agentplane.touchRecent('forged')), false)
    const again = await page.evaluate(() => window.agentplane.newGraph())
    assert.equal(again.ok, false)
    assert.equal(again.code, 'exists')
    const recent = await page.evaluate(() => window.agentplane.recentGraphs())
    assert.deepEqual(recent.map((r) => r.label), ['fresh.yaml', 'demo.yaml'])
    assert.equal(JSON.stringify(recent).includes(work), false)
    const viaRecent = await page.evaluate((id) => window.agentplane.openRecentGraph(id), recent[0].id)
    assert.equal(viaRecent.ok, true)
    assert.equal(viaRecent.label, 'fresh.yaml')

    // E2.1: the YAML editor. Open through the real dialog path, type, save with the keyboard.
    const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
    const typeInEditor = async (t) => {
      await page.click('[data-testid="yaml-content"]')
      await page.keyboard.press(`${mod}+End`)
      await page.keyboard.type(t)
    }
    const doc2 = path.join(graphDir, 'edit-me.yaml')
    fs.writeFileSync(doc2, original)
    await stubOpen(doc2)
    await page.click('[data-testid="nav-editor"]')
    await page.click('[data-testid="editor-open"]')
    await page.waitForSelector('[data-testid="yaml-content"]')
    assert.match(await page.textContent('[data-testid="doc-state"]'), /^Saved/i)
    await typeInEditor('\n# added in the editor\n')
    assert.match(await page.textContent('[data-testid="doc-state"]'), /Unsaved/i)
    await page.keyboard.press(`${mod}+s`)
    await page.waitForFunction(() => /^Saved/i.test(document.querySelector('[data-testid="doc-state"]')?.textContent || ''))
    assert.equal(fs.readFileSync(doc2, 'utf8'), original + '\n# added in the editor\n', 'Ctrl+S writes exactly the typed text')
    await page.click('[data-testid="editor-undo"]')
    assert.match(await page.textContent('[data-testid="doc-state"]'), /Unsaved/i)
    // Broken YAML is reported, not saved over silently.
    await page.click('[data-testid="yaml-content"]')
    await page.keyboard.press(`${mod}+End`)
    await page.keyboard.type('\nid: [\n')
    await page.waitForSelector('[data-testid="issue"][data-code="YAML"]')
    // The file changes on disk while there are unsaved edits: conflict, nothing is overwritten.
    fs.writeFileSync(doc2, original + '# elsewhere\n')
    await page.click('[data-testid="editor-save"]')
    await page.waitForSelector('[data-testid="conflict-banner"]')
    assert.equal(fs.readFileSync(doc2, 'utf8'), original + '# elsewhere\n')
    await page.click('[data-testid="reload"]')
    await page.waitForFunction(() => !document.querySelector('[data-testid="conflict-banner"]'))
    assert.match(await page.textContent('[data-testid="doc-state"]'), /^Saved/i)
    // Leaving with unsaved edits asks first.
    await typeInEditor('# draft\n')
    await page.click('[data-testid="editor-close"]')
    await page.waitForSelector('[data-testid="leave-prompt"]')
    await page.click('[data-testid="leave-cancel"]')
    await page.click('[data-testid="editor-close"]')
    await page.click('[data-testid="leave-discard"]')
    await page.waitForSelector('[data-testid="editor-empty"]')
    assert.equal(fs.readFileSync(doc2, 'utf8'), original + '# elsewhere\n', 'discard leaves the file alone')
    // A new graph is create-only and starts from a valid skeleton.
    await stubSave(path.join(graphDir, 'brand-new'))
    await page.click('[data-testid="editor-new"]')
    await page.waitForSelector('[data-testid="yaml-content"]')
    assert.equal(fs.existsSync(path.join(graphDir, 'brand-new.yaml')), false, 'nothing is written before the first save')
    await page.keyboard.press(`${mod}+s`)
    await page.waitForFunction(() => /^Saved/i.test(document.querySelector('[data-testid="doc-state"]')?.textContent || ''))
    assert.match(fs.readFileSync(path.join(graphDir, 'brand-new.yaml'), 'utf8'), /schema_version: 0/)
    await shot(page, '8-editor')
    await page.click('[data-testid="editor-close"]')
    await page.click('[data-testid="nav-dashboard"]')

    // E2.2: the canvas. Layout is not semantics; every action works from the keyboard.
    const key = async (testId, k = 'Enter') => {
      await page.focus(`[data-testid="${testId}"]`)
      await page.keyboard.press(k)
    }
    const canvasGraph = path.join(graphDir, 'canvas.yaml')
    fs.writeFileSync(canvasGraph, original)
    await stubOpen(canvasGraph)
    await page.click('[data-testid="nav-editor"]')
    await page.click('[data-testid="editor-open"]')
    await page.waitForSelector('[data-testid="node-fast"]')
    // Every edge of the demo is drawn (React Flow draws none when it loses handle measurements).
    await page.waitForFunction(() => document.querySelectorAll('.react-flow__edge').length === 3)
    const versionBefore = await page.textContent('text=/Version [0-9a-f]{8}/')
    const box = await page.locator('[data-testid="node-fast"]').boundingBox()
    await page.mouse.move(box.x + box.width / 2, box.y + 10)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width / 2 + 70, box.y + 50, { steps: 8 })
    await page.mouse.up()
    await page.waitForFunction(() => /Unsaved/i.test(document.querySelector('[data-testid="doc-state"]')?.textContent || ''))
    await page.keyboard.press(`${mod}+s`)
    await page.waitForFunction(() => /^Saved/i.test(document.querySelector('[data-testid="doc-state"]')?.textContent || ''))
    const moved = fs.readFileSync(canvasGraph, 'utf8')
    const added = moved.split('\n').filter((l) => !original.split('\n').includes(l))
    assert.ok(added.length > 0 && added.every((l) => /metadata:|layout:|nodes:|fast: \{x: -?\d+, y: -?\d+\}/.test(l)), `a move may only add layout lines, got ${JSON.stringify(added)}`)
    assert.ok(moved.startsWith('# Phase 2 vertical slice demo'), 'comments survive a move')
    assert.equal(await page.textContent('text=/Version [0-9a-f]{8}/'), versionBefore, 'moving a node never changes the semantic version')
    await page.waitForFunction(() => document.querySelectorAll('.react-flow__edge').length === 3)
    // Broken YAML keeps the last good canvas, read-only, and fixing it brings the canvas back.
    await page.click('[data-testid="yaml-content"]')
    await page.keyboard.press(`${mod}+End`)
    await page.keyboard.type('\nbroken: [\n')
    await page.waitForSelector('[data-testid="canvas-readonly"]')
    assert.equal(await page.locator('[data-testid="add-shell"]').isDisabled(), true)
    assert.equal(await page.isVisible('[data-testid="node-fast"]'), true, 'the last good graph stays on screen')
    await page.click('[data-testid="editor-undo"]')
    await page.waitForFunction(() => !document.querySelector('[data-testid="canvas-readonly"]'))
    await page.click('[data-testid="editor-close"]')
    // A file with anchors opens with a read-only canvas and an editable YAML view.
    const anchored = path.join(graphDir, 'anchors.yaml')
    fs.copyFileSync(path.join(__dirname, '..', '..', 'internal', 'adapter', 'yamlgraph', 'edit', 'testdata', 'hostile.yaml'), anchored)
    await stubOpen(anchored)
    await page.click('[data-testid="editor-open"]')
    await page.waitForSelector('[data-testid="canvas-readonly"]')
    assert.equal(await page.locator('[data-testid="add-shell"]').isDisabled(), true)
    assert.equal(await page.isVisible('[data-testid="yaml-content"]'), true)
    await page.click('[data-testid="editor-close"]')
    // Keyboard only: connect two nodes through the engine-checked list, add a node, rename it, save.
    const keyed = path.join(graphDir, 'keyboard.yaml')
    fs.writeFileSync(keyed, 'schema_version: 0\nid: keyboard\ninputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: "true"}\n    outputs:\n      stdout: {type: string}\n  b:\n    type: shell\n    config: {command: "true"}\n    inputs:\n      text: {type: string}\nedges: []\noutputs: {}\n')
    await stubOpen(keyed)
    await key('editor-open')
    await page.waitForSelector('[data-testid="node-a"]')
    await key('list-node-a')
    await key('op-connect')
    await key('connect-out-stdout')
    await page.waitForSelector('[data-testid="connect-to-b.inputs.text"]:not([disabled])')
    await key('connect-to-b.inputs.text')
    await page.waitForFunction(() => document.querySelectorAll('.react-flow__edge').length === 1)
    await key('add-shell')
    await page.waitForSelector('[data-testid="node-shell_1"]')
    await key('op-rename')
    await page.fill('[data-testid="rename-input"]', 'extra')
    await page.keyboard.press('Enter')
    await page.waitForSelector('[data-testid="node-extra"]')
    await page.keyboard.press(`${mod}+s`)
    await page.waitForFunction(() => /^Saved/i.test(document.querySelector('[data-testid="doc-state"]')?.textContent || ''))
    const keyedText = fs.readFileSync(keyed, 'utf8')
    assert.ok(keyedText.includes('from: a.outputs.stdout') && keyedText.includes('to: b.inputs.text'), keyedText)
    assert.ok(/^ {2}extra:/m.test(keyedText) && !keyedText.includes('shell_1'), 'the renamed node is in the file under its new id')
    // Deleting a connected node asks first and says what goes with it; undo restores the exact text.
    await key('list-node-b')
    await key('op-delete')
    await page.waitForSelector('[data-testid="delete-confirm"]')
    assert.match(await page.textContent('[data-testid="delete-confirm"]'), /1 connection/)
    await key('delete-apply')
    await page.waitForFunction(() => !document.querySelector('[data-testid="node-b"]'))
    await page.click('[data-testid="editor-undo"]')
    await page.waitForSelector('[data-testid="node-b"]')
    await shot(page, '9-canvas')
    assert.match(await page.textContent('[data-testid="doc-state"]'), /^Saved/i, 'undo after a delete restores the saved text exactly')
    await page.click('[data-testid="editor-close"]')
    // Pointer: dragging from an output handle to a compatible input handle connects them.
    const dragged = path.join(graphDir, 'drag.yaml')
    fs.copyFileSync(keyed, dragged)
    fs.writeFileSync(dragged, fs.readFileSync(keyed, 'utf8').replace(/edges:[\s\S]*?outputs: \{\}/, 'edges: []\noutputs: {}'))
    await stubOpen(dragged)
    await page.click('[data-testid="editor-open"]')
    await page.waitForSelector('[data-testid="node-a"]')
    const from = await page.locator('[data-testid="handle-a.outputs.stdout"]').boundingBox()
    const to = await page.locator('[data-testid="handle-b.inputs.text"]').boundingBox()
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
    await page.mouse.down()
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 12 })
    await page.mouse.up()
    await page.waitForFunction(() => document.querySelectorAll('.react-flow__edge').length === 1)
    assert.ok((await page.locator('[data-testid="yaml-content"]').textContent()).includes('a.outputs.stdout'), 'the YAML shows the new edge')
    await page.click('[data-testid="editor-close"]')
    await page.click('[data-testid="leave-discard"]')
    await page.click('[data-testid="nav-dashboard"]')

    // E2.3: window focus finds outside changes; Save and run runs the SAVED file.
    const focusIt = () => page.evaluate(() => window.dispatchEvent(new Event('focus')))
    const watched = path.join(graphDir, 'watched.yaml')
    fs.writeFileSync(watched, original)
    await stubOpen(watched)
    await page.click('[data-testid="nav-editor"]')
    await page.click('[data-testid="editor-open"]')
    await page.waitForSelector('[data-testid="node-fast"]')
    // A clean draft follows the file silently.
    fs.writeFileSync(watched, original + '# changed outside\n')
    await focusIt()
    await page.waitForFunction(() => document.querySelector('[data-testid="yaml-content"]')?.textContent?.includes('changed outside'))
    assert.equal(await page.isVisible('[data-testid="conflict-banner"]'), false)
    // A dirty draft is told, never overwritten; Keep mine saves over the newer file.
    await page.click('[data-testid="yaml-content"]')
    await page.keyboard.press(`${mod}+End`)
    await page.keyboard.type('# mine\n')
    fs.writeFileSync(watched, original + '# changed again\n')
    await focusIt()
    await page.waitForSelector('[data-testid="conflict-banner"]')
    assert.equal(fs.readFileSync(watched, 'utf8'), original + '# changed again\n')
    // Save and run is refused by the same conflict and starts nothing.
    const runsBefore = (await page.evaluate(() => window.agentplane.request('GET', '/runs'))).body.runs.length
    await page.click('[data-testid="editor-save-run"]')
    await page.waitForSelector('[data-testid="conflict-banner"]')
    assert.equal((await page.evaluate(() => window.agentplane.request('GET', '/runs'))).body.runs.length, runsBefore, 'a refused save starts no run')
    await page.click('[data-testid="keep-mine"]')
    await page.waitForFunction(() => !document.querySelector('[data-testid="conflict-banner"]'))
    assert.ok(fs.readFileSync(watched, 'utf8').endsWith('# mine\n'))
    await page.click('[data-testid="editor-close"]')

    // Round trip: a graph made in the editor is saved, run from the app, and the CLI accepts the same file.
    const made2 = path.join(graphDir, 'roundtrip')
    await stubSave(made2)
    await page.click('[data-testid="editor-new"]')
    await page.waitForSelector('[data-testid="yaml-content"]')
    await page.click('[data-testid="add-shell"]')
    await page.waitForSelector('[data-testid="node-shell_1"]')
    assert.equal(await page.locator('[data-testid="editor-save-run"]').isDisabled(), true, 'a starter shell has an empty command, so the engine blocks the run')
    await page.click('[data-testid="yaml-content"]')
    await page.keyboard.press(`${mod}+a`)
    // insertText keeps the text exactly; typing Enter in CodeMirror would auto-indent.
    await page.evaluate((t) => document.execCommand('insertText', false, t), 'schema_version: 0\nid: roundtrip\ninputs: {}\nnodes:\n  say:\n    type: shell\n    config: {command: /usr/bin/printf, args: ["hello\\n"]}\nedges: []\noutputs: {}\n')
    await page.waitForFunction(() => !document.querySelector('[data-testid="run-blocked"]'))
    await page.click('[data-testid="editor-save-run"]')
    await page.waitForSelector('[data-testid="run-status"]')
    await page.waitForFunction(() => document.querySelector('[data-testid="run-status"]')?.textContent === 'Succeeded', null, { timeout: 20000 })
    const exec = require('node:child_process').execFileSync
    const bin = path.join(__dirname, '..', '..', 'bin', 'agentplane')
    exec(bin, ['graph', 'validate', path.join(graphDir, 'roundtrip.yaml')])
    exec(bin, ['run', path.join(graphDir, 'roundtrip.yaml'), '--store-dir', path.join(work, 'cli-runs')])
    await page.click('[data-testid="nav-editor"]')
    await page.click('[data-testid="editor-close"]').catch(() => {})
    await page.click('[data-testid="nav-dashboard"]')

    // F2.4: the Issues panel. Config errors show before any run; issues take you to their element and line.
    const activeId = () => page.evaluate(() => document.activeElement?.getAttribute('data-testid'))
    const mixedText = 'schema_version: 0\nid: mixed\ninputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: ""}\n    outputs:\n      out: {type: string}\n      spare: {type: string}\n  b:\n    type: shell\n    config: {command: /usr/bin/true}\n    inputs:\n      n: {type: number, required: true}\n      free: {type: string, required: true}\nedges:\n  - {from: a.outputs.out, to: b.inputs.n}\noutputs: {}\n'
    const issuesFile = path.join(graphDir, 'issues.yaml')
    fs.writeFileSync(issuesFile, mixedText)
    await stubOpen(issuesFile)
    await page.click('[data-testid="nav-editor"]')
    await page.click('[data-testid="editor-open"]')
    await page.waitForSelector('[data-testid="issue"]')
    // Same diagnostics as the CLI, and the config error is there before any run.
    const cliOut = JSON.parse(require('node:child_process').spawnSync(path.join(__dirname, '..', '..', 'bin', 'agentplane'), ['graph', 'validate', issuesFile, '--json'], { encoding: 'utf8' }).stdout)
    const panelCodes = await page.$$eval('[data-testid="issue"]', (els) => els.map((e) => e.getAttribute('data-code')).sort())
    assert.deepEqual(panelCodes, cliOut.diagnostics.map((d) => d.code).sort(), 'the panel shows exactly the CLI diagnostics')
    assert.ok(panelCodes.includes('E_NODE_CONFIG_INVALID'))
    // Errors first.
    const severities = await page.$$eval('[data-testid="issue"]', (els) => els.map((e) => e.getAttribute('data-severity')))
    assert.deepEqual(severities, [...severities].sort((x, y) => (x === 'error' ? 0 : 1) - (y === 'error' ? 0 : 1)))
    // Run is blocked by errors, names the count, and its action focuses the first error.
    assert.equal(await page.locator('[data-testid="editor-save-run"]').isDisabled(), true)
    assert.match(await page.textContent('[data-testid="run-blocked"]'), /4 errors/)
    await page.click('[data-testid="run-blocked-focus"]')
    await page.waitForFunction(() => document.activeElement?.getAttribute('data-testid') === 'selected-heading')
    // Filter, badges and focus on an edge issue, with the YAML scrolled to its line.
    await page.click('[data-testid="issues-filter-warning"]')
    assert.equal(await page.locator('[data-testid="issue"][data-severity="error"]').count(), 0)
    await page.click('[data-testid="issues-filter-all"]')
    assert.ok((await page.textContent('[data-testid="node-a"]')).includes('✖'), 'a node with an error carries a glyph, not only colour')
    await page.click('[data-testid="issue"][data-code="E_BINDING_TYPE_MISMATCH"]')
    await page.waitForFunction(() => document.querySelector('[data-testid="selected-heading"]')?.textContent?.includes('a.outputs.out'))
    assert.match(await page.textContent('.cm-activeLine'), /from: a\.outputs\.out/)
    await page.click('[data-testid="issue"][data-code="E_NODE_CONFIG_INVALID"]')
    await page.waitForFunction(() => document.querySelector('.cm-activeLine')?.textContent?.includes('config:'))
    // Typing broken YAML shows one blocking issue with its line, quickly, and fixing it clears it.
    await page.click('[data-testid="yaml-content"]')
    await page.keyboard.press(`${mod}+End`)
    await page.keyboard.type('x: [')
    // Measured inside the page, from the last keystroke to the issue being in the DOM (Playwright's own polling is not part of the product).
    const parseMs = await page.evaluate(
      () =>
        new Promise((resolve) => {
          const t0 = performance.now()
          const done = () => document.querySelector('[data-testid="issue"][data-code="YAML"]') && (obs.disconnect(), resolve(performance.now() - t0))
          const obs = new MutationObserver(done)
          obs.observe(document.body, { childList: true, subtree: true })
          document.execCommand('insertText', false, '\n')
        }),
    )
    assert.ok(parseMs < 500, `the parse error must show within 500 ms of the last keystroke, took ${parseMs} ms`)
    assert.equal(await page.locator('[data-testid="issue"]').count(), 1, 'a text that does not parse has one issue')
    assert.match(await page.textContent('[data-testid="issue"]'), /line \d+/)
    await page.click('[data-testid="editor-undo"]')
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="issue"][data-code="YAML"]').length === 0)
    await page.click('[data-testid="editor-close"]')
    // Graph-level issue focuses the graph header.
    const badId = path.join(graphDir, 'badid.yaml')
    fs.writeFileSync(badId, 'schema_version: 0\nid: Bad_ID\ninputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: /usr/bin/true}\nedges: []\noutputs: {}\n')
    await stubOpen(badId)
    await page.click('[data-testid="editor-open"]')
    await page.click('[data-testid="issue"][data-code="E_GRAPH_ID_INVALID"]')
    await page.waitForFunction(() => document.activeElement?.getAttribute('data-testid') === 'graph-heading')
    await page.click('[data-testid="editor-close"]')
    // Warnings never block a run.
    const warnOnly = path.join(graphDir, 'warn.yaml')
    fs.writeFileSync(warnOnly, 'schema_version: 0\nid: warn\ninputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: /usr/bin/true}\n    outputs:\n      o: {type: string}\nedges: []\noutputs: {}\n')
    await stubOpen(warnOnly)
    await page.click('[data-testid="editor-open"]')
    await page.waitForSelector('[data-testid="issue"][data-severity="warning"]')
    assert.equal(await page.locator('[data-testid="editor-save-run"]').isDisabled(), false)
    await page.click('[data-testid="editor-close"]')
    // Keyboard only: filter, move through the issues, focus one, and land on its inspector heading.
    await stubOpen(issuesFile)
    await key('editor-open')
    await page.waitForSelector('[data-testid="issue"]')
    await key('issues-filter-error')
    assert.equal(await page.locator('[data-testid="issue"][data-severity="warning"]').count(), 0)
    await page.focus('[data-testid="issue"]')
    await page.keyboard.press('Tab')
    assert.equal(await activeId(), 'issue')
    await page.keyboard.press('Enter')
    await page.waitForFunction(() => ['selected-heading', 'graph-heading'].includes(document.activeElement?.getAttribute('data-testid') || ''))
    await page.click('[data-testid="editor-close"]')
    await page.click('[data-testid="nav-dashboard"]')

    // F2.2: the node inspector, keyboard only. Every change is an operation on the file.
    const inspected = path.join(graphDir, 'inspect.yaml')
    fs.writeFileSync(inspected, 'schema_version: 0\nid: inspect\ninputs: {}\nnodes:\n  # the producer\n  build:\n    type: shell\n    config:\n      command: /usr/bin/true\n      retries: {max: 2, on: [timeout]}\n    outputs:\n      stdout: {type: string}\nedges: []\noutputs: {}\n')
    await stubOpen(inspected)
    await page.click('[data-testid="nav-editor"]')
    await key('editor-open')
    await page.waitForSelector('[data-testid="node-build"]')
    await key('list-node-build')
    await page.waitForSelector('[data-testid="inspector-node"]')
    const yamlText = () => page.locator('[data-testid="yaml-content"]').textContent()
    const waitYaml = (s) => page.waitForFunction((x) => document.querySelector('[data-testid="yaml-content"]')?.textContent?.includes(x), s)
    const commandInput = '[data-testid="field-shell.config.command"] input'
    // Change the command: typing alone sends nothing, Enter commits one operation.
    await page.focus(commandInput)
    await page.keyboard.press(`${mod}+a`)
    await page.keyboard.type('/usr/bin/printf')
    assert.ok(!(await yamlText()).includes('/usr/bin/printf'), 'typing alone does not edit the file')
    await page.keyboard.press('Enter')
    await waitYaml('command: /usr/bin/printf')
    // Add an argument through the list editor.
    await page.focus('[data-testid="list-args-add"]')
    await page.keyboard.type('{"ok":true}')
    await page.keyboard.press('Enter')
    await waitYaml('args:')
    // Toggle stdout_json with Space, then declare the outputs it allows.
    await page.focus('[data-testid="field-shell.config.stdout_json"] input[type="checkbox"]')
    await page.keyboard.press('Space')
    await waitYaml('stdout_json: true')
    await page.focus('[data-testid="add-port-outputs"]')
    await page.selectOption('[data-testid="add-port-outputs"]', 'json')
    await page.waitForSelector('[data-testid="port-outputs-json"]')
    await page.selectOption('[data-testid="add-port-outputs"]', 'stderr')
    await page.waitForSelector('[data-testid="port-outputs-stderr"]')
    // Enable a loop: the starter binds the json output to a json_predicate Gate.
    await key('loop-enable')
    await page.waitForSelector('[data-testid="loop-on"]')
    await page.keyboard.press(`${mod}+s`)
    await page.waitForFunction(() => /^Saved/i.test(document.querySelector('[data-testid="doc-state"]')?.textContent || ''))
    const inspectedText = fs.readFileSync(inspected, 'utf8')
    for (const want of ['command: /usr/bin/printf', 'stdout_json: true', 'stderr:', 'json:', 'kind: json_predicate', 'candidate.outputs.json', 'retries: {max: 2, on: [timeout]}', '# the producer']) {
      assert.ok(inspectedText.includes(want), `${want} in\n${inspectedText}`)
    }
    // The saved file is accepted by the CLI, apart from the Advanced key the Shell executor rejects.
    const validated = JSON.parse(require('node:child_process').spawnSync(path.join(__dirname, '..', '..', 'bin', 'agentplane'), ['graph', 'validate', inspected, '--json'], { encoding: 'utf8' }).stdout)
    assert.deepEqual(validated.diagnostics.filter((d) => d.severity === 'error').map((d) => d.message.includes('retries')), [true], JSON.stringify(validated.diagnostics))
    // The Advanced section shows the unknown key as it is in the file; removing it makes the graph valid.
    assert.match(await page.textContent('[data-testid="advanced-entry-retries"]'), /\{max: 2, on: \[timeout\]\}/)
    await key('advanced-remove-retries')
    await page.waitForFunction(() => !document.querySelector('[data-testid="advanced-entry-retries"]'))
    await page.waitForFunction(() => !document.querySelector('[data-testid="run-blocked"]'))
    // An invalid value is saved as typed, its diagnostic is shown in the inspector, and Run is disabled.
    await page.focus(commandInput)
    await page.keyboard.press(`${mod}+a`)
    await page.keyboard.press('Backspace')
    await page.keyboard.press('Enter')
    await waitYaml('command: ""')
    await page.waitForSelector('[data-testid="section-execution"] [data-testid="inspector-diagnostics"]')
    assert.match(await page.textContent('[data-testid="inspector-status"]'), /1 error/)
    assert.equal(await page.locator('[data-testid="editor-save-run"]').isDisabled(), true)
    await page.keyboard.press(`${mod}+s`)
    await page.waitForFunction(() => /^Saved/i.test(document.querySelector('[data-testid="doc-state"]')?.textContent || ''))
    assert.ok(fs.readFileSync(inspected, 'utf8').includes('command: ""'), 'the invalid value is in the file, not dropped')
    assert.equal(await page.locator('[data-testid="editor-save-run"]').isDisabled(), true)
    await shot(page, '10-inspector')
    await page.click('[data-testid="editor-close"]')
    await page.click('[data-testid="nav-dashboard"]')

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
    await startDemo(app, page)
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
    assert.equal(await page.locator('[data-testid="run-row"]').count(), 2, "the demo run and the run started from the editor")
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
    await startDemo(app, page)
    await waitStatus(page, 'approve', 'waiting_human')
    await page.click('[data-testid="node-approve"]')
    await page.click('[data-testid="approval-box"] button.danger')
    await page.waitForFunction(() => document.querySelector('[data-testid="run-status"]')?.textContent === 'Failed')
    assert.equal(await status(page, 'approve'), 'failed')
    assert.equal(await status(page, 'apply'), 'skipped')
    await shot(page, '6-rejected')

    // #56: New run chooses the graph in the native dialog and holds only a grant. It starts the run
    // from the file as saved when the run starts, and the renderer cannot send a path at all.
    const say = (word) => `schema_version: 0\nid: newrun\ninputs: {}\nnodes:\n  say:\n    type: shell\n    config: {command: /usr/bin/printf, args: ["${word}\\n"]}\nedges: []\noutputs: {}\n`
    const chosen = path.join(graphDir, 'new-run.yaml')
    fs.writeFileSync(chosen, say('first'))
    await stubOpenDialog(app, chosen)
    await page.click('[data-testid="nav-dashboard"]')
    await page.click('[data-testid="quick-open"]')
    await page.waitForFunction(() => document.querySelector('[data-testid="preflight-status"]')?.textContent === 'Ready')
    assert.equal(await page.textContent('[data-testid="graph-label"]'), 'new-run.yaml', 'Open graph on the Dashboard lands on New run with the file chosen')
    await page.click('[data-testid="nav-new"]')
    assert.equal(await page.locator('[data-testid="graph-path"]').count(), 0, 'New run has no field for a path')
    await page.click('[data-testid="choose"]')
    await page.waitForFunction(() => document.querySelector('[data-testid="preflight-status"]')?.textContent === 'Ready')
    assert.equal(await page.textContent('[data-testid="graph-label"]'), 'new-run.yaml')
    assert.equal((await page.content()).includes(work), false, 'the page never shows the path')
    // The file is saved again after it was chosen and checked: the run reads the saved bytes.
    fs.writeFileSync(chosen, say('second'))
    await page.click('[data-testid="run"]')
    await page.waitForFunction(() => document.querySelector('[data-testid="run-status"]')?.textContent === 'Succeeded', null, { timeout: 20000 })
    const listed = (await page.evaluate(() => window.agentplane.request('GET', '/runs'))).body.runs
    const started = listed.filter((r) => r.graph_id === 'newrun')
    assert.equal(started.length, 1)
    const detail = await page.evaluate((id) => window.agentplane.request('GET', `/runs/${id}`), started[0].run_id)
    assert.ok(detail.body.graph.includes('second') && !detail.body.graph.includes('first'), 'the run used the saved bytes of the chosen file')
    // A path is refused by main before it reaches the engine, on both routes, in any key spelling.
    for (const [route, body] of [['/runs', { graph_path: chosen, inputs: [] }], ['/runs', { GRAPH_PATH: chosen }], ['/graphs/validate', { path: chosen }]]) {
      const refusedBody = await page.evaluate(([r, b]) => window.agentplane.request('POST', r, b), [route, body])
      assert.equal(refusedBody.status, 400, `${route} ${Object.keys(body)}`)
      assert.equal(refusedBody.body.error.message, 'body not allowed')
    }
    const afterRefusals = (await page.evaluate(() => window.agentplane.request('GET', '/runs'))).body.runs
    assert.equal(afterRefusals.length, listed.length, 'a refused path starts no run')

    // 6. If the engine dies mid-run the UI says the connection was lost and keeps
    //    the last events it received; it must not invent a run status.
    await startDemo(app, page)
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
