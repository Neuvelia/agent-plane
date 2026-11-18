'use strict'
// node scripts/record-graph-fixtures.cjs   (needs `make build`)
// Records real answers of the graph document API (ADR-0021) for Storybook, so
// stories show what the engine actually says. Writes test/fixtures/graph-*.json.
const { spawn } = require('node:child_process')
const crypto = require('node:crypto')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')

const root = path.resolve(__dirname, '..', '..')
const out = path.resolve(__dirname, '..', 'test', 'fixtures')

const GRAPHS = {
  // valid, with the engine's "declared output not consumed" warning
  pair: `# Two shell nodes, not yet connected.
schema_version: 0
id: pair
inputs: {}

nodes:
  # the producer
  a:
    type: shell
    config:
      command: "true"
    outputs:
      stdout: {type: string}

  b:
    type: shell
    config:
      command: "true"
    inputs:
      text: {type: string}

edges: []
outputs: {}
`,
  // a node whose configuration the executors reject
  'config-error': `schema_version: 0
id: config-error
inputs: {}
nodes:
  work:
    type: shell
    config:
      command: ""
    outputs:
      stdout: {type: string}
edges: []
outputs:
  result: {from: work.outputs.stdout}
`,
  // a graph that declares a required input
  'with-inputs': `schema_version: 0
id: with-inputs
inputs:
  topic: {type: string, required: true}
nodes:
  echo:
    type: shell
    config: {command: /usr/bin/printf, args: ["%s\\n"]}
    inputs:
      topic: {type: string, required: true}
graph_input_bindings:
  - {from: inputs.topic, to: echo.inputs.topic}
edges: []
outputs: {}
`,
  // errors and warnings on a node, an edge and ports
  mixed: `schema_version: 0
id: mixed
inputs: {}
nodes:
  a:
    type: shell
    config: {command: ""}
    outputs:
      out: {type: string}
      spare: {type: string}
  b:
    type: shell
    config: {command: /usr/bin/true}
    inputs:
      n: {type: number, required: true}
      free: {type: string, required: true}
edges:
  - {from: a.outputs.out, to: b.inputs.n}
outputs: {}
`,
  // what a new graph starts as
  empty: 'schema_version: 0\nid: new-graph\ninputs: {}\nnodes: {}\nedges: []\noutputs: {}\n',
  // YAML the engine cannot read
  'parse-error': 'schema_version: 0\nid: [\nnodes: {}\n',
}

;(async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'ap-fixtures-'))
  const child = spawn(path.join(root, 'bin', 'agentplane'), ['serve', '--store-dir', path.join(dir, 'runs')], { stdio: ['pipe', 'pipe', 'inherit'] })
  const info = await new Promise((resolve, reject) => {
    let buf = ''
    child.stdout.on('data', (c) => {
      buf += c
      const nl = buf.indexOf('\n')
      if (nl >= 0) resolve(JSON.parse(buf.slice(0, nl)))
    })
    child.on('error', reject)
  })
  const call = async (method, p, body) => {
    const r = await fetch(`http://127.0.0.1:${info.port}${p}`, { method, headers: { Authorization: `Bearer ${info.token}`, 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
    return { status: r.status, body: await r.json() }
  }
  const write = (name, data) => fs.writeFileSync(path.join(out, name), JSON.stringify(data, null, 1) + '\n')
  try {
    // F2.12: the engine's answer for a draft of the agent template, whose workspace is the one value to fill.
    // AP_RECORD=templates records only this.
    const template = fs.readFileSync(path.join(root, '.agents', 'templates', 'agent-scoped-approval.yaml'), 'utf8').replace(/^id: agent-scoped-approval$/m, 'id: new-graph')
    write('graph-validate-template-agent-scoped-approval.json', (await call('POST', '/graphs/validate', { text: template })).body)
    if (process.env.AP_RECORD === 'templates') return
    // F2.11: this machine's capability report; stories derive the other states from it.
    fs.writeFileSync(path.join(out, 'capabilities-linux.json'), JSON.stringify((await call('GET', '/capabilities')).body, null, 2) + '\n')
    if (process.env.AP_RECORD === 'capabilities') return
    for (const [name, text] of Object.entries(GRAPHS)) {
      const file = path.join(dir, `${name}.yaml`)
      fs.writeFileSync(file, text)
      const grant = (await call('POST', '/graphs/grants', { path: file, mode: 'open' })).body.grant
      const opened = await call('POST', '/graphs/open', { grant })
      if (opened.status !== 200) throw new Error(`open ${name}: ${opened.status}`)
      write(`graph-open-${name}.json`, opened.body)
      const validated = await call('POST', '/graphs/validate', { text })
      write(`graph-validate-${name}.json`, validated.body)
    }
    // The canvas (E2.2): the demo graph, a file with anchors, and real answers to
    // the operations the canvas sends.
    const demoText = fs.readFileSync(path.join(root, '.agents', 'graphs', 'demo-vertical.yaml'), 'utf8')
    const hostile = fs.readFileSync(path.join(root, 'internal', 'adapter', 'yamlgraph', 'edit', 'testdata', 'hostile.yaml'), 'utf8')
    for (const [name, text] of [['demo', demoText], ['anchors', hostile]]) {
      const file = path.join(dir, `${name}.yaml`)
      fs.writeFileSync(file, text)
      const grant = (await call('POST', '/graphs/grants', { path: file, mode: 'open' })).body.grant
      const opened = await call('POST', '/graphs/open', { grant })
      write(`graph-open-${name}.json`, opened.body)
    }
    const edit = async (name, text, ops) => {
      const file = path.join(dir, 'edit.yaml')
      fs.writeFileSync(file, text)
      const grant = (await call('POST', '/graphs/grants', { path: file, mode: 'open' })).body.grant
      const r = await call('POST', '/graphs/edit', { grant, base_hash: crypto.createHash('sha256').update(text).digest('hex'), text, ops_version: 1, ops })
      write(`graph-edit-${name}.json`, r)
    }
    const pair = GRAPHS.pair
    await edit('add-node', pair, [{ op: 'add_node', id: 'shell_1', type: 'shell' }, { op: 'set_layout', nodes: { shell_1: { x: 80, y: 60 } } }])
    await edit('connect', pair, [{ op: 'add_edge', from: 'a.outputs.stdout', to: 'b.inputs.text' }])
    await edit('refused', pair, [{ op: 'add_edge', from: 'a.outputs.stdout', to: 'nope.inputs.x' }])
    await edit('move', pair, [{ op: 'set_layout', nodes: { a: { x: 40, y: 40 } } }])
    const opened = JSON.parse(fs.readFileSync(path.join(out, 'graph-open-demo.json'), 'utf8'))
    const targets = Object.entries(opened.definition.nodes).flatMap(([id, n]) => Object.keys(n.inputs || {}).map((p) => `${id}.inputs.${p}`))
    await edit('can-connect-demo', demoText, targets.map((to) => ({ op: 'can_connect', from: 'fast.outputs.stdout', to })))
    await edit('rename-demo', demoText, [{ op: 'rename_node', id: 'fast', new_id: 'quick' }])
    await edit('delete-demo', demoText, [{ op: 'remove_node', id: 'flaky' }])
  } finally {
    child.stdin.end()
    setTimeout(() => child.kill(), 500).unref()
  }
  console.log('recorded graph fixtures in', out)
})().catch((e) => {
  console.error(e)
  process.exit(1)
})
