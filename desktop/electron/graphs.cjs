'use strict'
// Main-process handlers for choosing graph files (ADR-0021). The person picks a
// file in a native dialog; main registers its path with the sidecar as a grant
// and returns only the grant and the file name. The renderer never learns or
// supplies a path for writing, and /graphs/grants is not on its allowlist.
const path = require('node:path')

const FILTERS = [{ name: 'Graph', extensions: ['yaml', 'yml'] }]

function failure(r) {
  const e = r.body && r.body.error
  return { ok: false, code: (e && e.code) || 'unavailable', message: (e && e.message) || `request failed (${r.status})` }
}

/**
 * deps.request(method, path, body) talks to the sidecar (main only).
 * deps.showOpen / deps.showSave(options) wrap the native dialogs.
 */
function createGraphHandlers({ request, showOpen, showSave, recents }) {
  const paths = new Map() // grant -> path, held in main only, for the recent list

  async function grant(filePath, mode) {
    const r = await request('POST', '/graphs/grants', { path: filePath, mode })
    if (r.status !== 201 || !r.body || typeof r.body.grant !== 'string') return failure(r)
    paths.set(r.body.grant, filePath)
    if (mode === 'open') recents.add(filePath)
    return { ok: true, grant: r.body.grant, label: r.body.label, exists: r.body.exists === true }
  }

  return {
    async openGraph() {
      const result = await showOpen({ title: 'Open a graph', properties: ['openFile'], filters: FILTERS })
      if (result.canceled || !result.filePaths[0]) return { canceled: true }
      return grant(result.filePaths[0], 'open')
    },

    async newGraph() {
      const result = await showSave({ title: 'Create a graph', defaultPath: 'graph.yaml', filters: FILTERS })
      if (result.canceled || !result.filePath) return { canceled: true }
      let target = result.filePath
      if (!/\.ya?ml$/i.test(path.extname(target))) target += '.yaml'
      return grant(target, 'create')
    },

    recentGraphs() {
      return recents.list()
    },

    async openRecentGraph(id) {
      const filePath = typeof id === 'string' ? recents.pathOf(id) : undefined
      if (!filePath) return { ok: false, code: 'not_found', message: 'that graph is no longer in the list' }
      const result = await grant(filePath, 'open')
      if (!result.ok && result.code === 'not_found') recents.remove(id) // the file is gone
      return result
    },

    /** After a first save the new file belongs in the recent list. Only a grant main issued counts. */
    touchRecent(grantId) {
      const filePath = typeof grantId === 'string' ? paths.get(grantId) : undefined
      if (filePath) recents.add(filePath)
      return Boolean(filePath)
    },

    async closeGraph(grantId) {
      if (typeof grantId !== 'string' || !paths.delete(grantId)) return false
      const r = await request('DELETE', `/graphs/grants/${encodeURIComponent(grantId)}`)
      return r.status === 204
    },
  }
}

module.exports = { createGraphHandlers }
