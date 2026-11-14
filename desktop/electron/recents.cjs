'use strict'
// Recently opened graphs, kept in the user's data directory by the main
// process. The renderer is only ever given ids and file names, never paths.
const fs = require('node:fs')
const path = require('node:path')
const crypto = require('node:crypto')

const MAX = 10

function createRecents(file, deps = {}) {
  const now = deps.now ?? Date.now
  const randomId = deps.randomId ?? (() => crypto.randomBytes(8).toString('hex'))

  function load() {
    try {
      const data = JSON.parse(fs.readFileSync(file, 'utf8'))
      if (data && data.version === 1 && Array.isArray(data.items)) {
        return data.items.filter((i) => i && typeof i.id === 'string' && typeof i.path === 'string' && typeof i.label === 'string')
      }
    } catch {
      // missing or corrupt: start empty
    }
    return []
  }

  function save(items) {
    try {
      fs.mkdirSync(path.dirname(file), { recursive: true })
      const tmp = `${file}.${process.pid}.tmp`
      fs.writeFileSync(tmp, JSON.stringify({ version: 1, items }), { mode: 0o600 })
      fs.renameSync(tmp, file)
    } catch {
      // history is a convenience: failing to write it never blocks editing
    }
  }

  return {
    /** Ids and labels, newest first. No paths. */
    list() {
      return load().map(({ id, label }) => ({ id, label }))
    },
    pathOf(id) {
      return load().find((i) => i.id === id)?.path
    },
    add(filePath) {
      const kept = load().filter((i) => i.path !== filePath)
      kept.unshift({ id: randomId(), path: filePath, label: path.basename(filePath), openedAt: now() })
      save(kept.slice(0, MAX))
    },
    remove(id) {
      save(load().filter((i) => i.id !== id))
    },
  }
}

module.exports = { createRecents, MAX }
