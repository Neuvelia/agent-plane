'use strict'
// "Record run values" (ADR-0023, F2.3 E6): a main-process setting, off by
// default, that decides the flags `agentplane serve` is started with. The
// renderer only ever sees the switch, the variable names and whether recording
// is active in this session; environment values never leave this process.
const fs = require('node:fs')
const path = require('node:path')

const MAX_NAMES = 16
const NAME = /^[A-Za-z_][A-Za-z0-9_]{0,127}$/
const DEFAULTS = Object.freeze({ enabled: false, redactEnv: [] })

/** Returns normalized settings, or { invalid } naming the first rejected entry. */
function validate(input) {
  if (!input || typeof input !== 'object' || Array.isArray(input)) return { invalid: 'settings' }
  const keys = Object.keys(input)
  if (keys.some((k) => k !== 'enabled' && k !== 'redactEnv')) return { invalid: 'settings' }
  if (typeof input.enabled !== 'boolean' || !Array.isArray(input.redactEnv)) return { invalid: 'settings' }
  if (input.redactEnv.length > MAX_NAMES) return { invalid: 'too_many' }
  const names = []
  for (const name of input.redactEnv) {
    if (typeof name !== 'string' || !NAME.test(name)) return { invalid: typeof name === 'string' ? name.slice(0, 64) : 'settings' }
    if (!names.includes(name)) names.push(name)
  }
  return { settings: { enabled: input.enabled, redactEnv: names } }
}

function createRecordingSettings(file) {
  return {
    /** Missing, unreadable or invalid storage means the safe default: off. */
    load() {
      try {
        const data = JSON.parse(fs.readFileSync(file, 'utf8'))
        if (data && data.version === 1) {
          const { settings } = validate({ enabled: data.enabled, redactEnv: data.redactEnv })
          if (settings) return settings
        }
      } catch {
        // fall through to the default
      }
      return { ...DEFAULTS, redactEnv: [] }
    },
    /** Returns { ok: true, settings } or { ok: false, code, name? }. */
    save(input) {
      const { settings, invalid } = validate(input)
      if (!settings) return { ok: false, code: 'invalid', name: invalid }
      try {
        fs.mkdirSync(path.dirname(file), { recursive: true })
        const tmp = `${file}.${process.pid}.tmp`
        fs.writeFileSync(tmp, JSON.stringify({ version: 1, ...settings }), { mode: 0o600 })
        fs.renameSync(tmp, file)
      } catch {
        return { ok: false, code: 'save_failed' }
      }
      return { ok: true, settings }
    },
  }
}

/**
 * The flags for `agentplane serve`. Only set variables are passed, because
 * serve refuses to start on an unset --redact-env name. If any configured name
 * is unset, recording stays off for this session: values are never recorded
 * with a redaction list that silently lost an entry.
 */
function serveFlags(settings, env) {
  const args = []
  const missing = []
  for (const name of settings.redactEnv) {
    if (Object.prototype.hasOwnProperty.call(env, name) && env[name] !== undefined) args.push('--redact-env', name)
    else missing.push(name)
  }
  const recording = settings.enabled && missing.length === 0
  if (recording) args.push('--record-values')
  return { args, recording, missing }
}

module.exports = { createRecordingSettings, serveFlags, validate, MAX_NAMES }
