'use strict'
// node scripts/record-graph-fixtures.cjs   (needs `make build`)
// Records real answers of the graph document API (ADR-0021) for Storybook, so
// stories show what the engine actually says. Writes test/fixtures/graph-*.json.
const { spawn } = require('node:child_process')
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
  } finally {
    child.stdin.end()
    setTimeout(() => child.kill(), 500).unref()
  }
  console.log('recorded graph fixtures in', out)
})().catch((e) => {
  console.error(e)
  process.exit(1)
})
