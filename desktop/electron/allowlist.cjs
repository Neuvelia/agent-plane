'use strict'
// The exact API surface the renderer may reach (ADR-0021). Anything else,
// including /graphs/grants (which only the main process calls), is refused here
// and never forwarded to the sidecar. Ids are the characters the engine issues;
// they cannot contain '.', '%' or '/' in a way that would climb out of a route
// ("..", "%2e%2e") because an id must start with a letter or digit.
const ID = '[A-Za-z0-9][A-Za-z0-9._-]*'

const RULES = [
  ['GET', /^\/runs$/],
  ['POST', /^\/runs$/],
  ['GET', new RegExp(`^/runs/${ID}$`)],
  ['POST', new RegExp(`^/runs/${ID}/decisions/${ID}$`)],
  ['GET', /^\/approvals$/],
  ['POST', /^\/graphs\/(validate|open|stat|edit)$/],
  ['PUT', /^\/graphs\/save$/],
  // Read-only capability report (F2.11, ADR-0025).
  ['GET', /^\/capabilities$/],
  ['POST', /^\/capabilities\/check$/],
]

function isAllowed(method, requestPath) {
  if (typeof method !== 'string' || typeof requestPath !== 'string') return false
  return RULES.some(([m, re]) => m === method && re.test(requestPath))
}

// The two routes whose body can name a graph file accept exactly these keys from
// the renderer, so a file is addressed by grant (or validated as text) and never
// by path. This is a key allow-list, not a deny-list: the Go API decodes JSON keys
// case-insensitively, so "GRAPH_PATH" would reach `graph_path`.
const GRAPH_BODIES = [
  ['POST', /^\/runs$/, ['graph_grant', 'inputs']],
  ['POST', /^\/graphs\/validate$/, ['grant', 'text']],
]

function isBodyAllowed(method, requestPath, body) {
  const rule = GRAPH_BODIES.find(([m, re]) => m === method && re.test(requestPath))
  if (!rule) return true
  if (body === null || typeof body !== 'object' || Array.isArray(body)) return false
  return Object.keys(body).every((key) => rule[2].includes(key))
}

module.exports = { isAllowed, isBodyAllowed }
