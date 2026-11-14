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
]

function isAllowed(method, requestPath) {
  if (typeof method !== 'string' || typeof requestPath !== 'string') return false
  return RULES.some(([m, re]) => m === method && re.test(requestPath))
}

module.exports = { isAllowed }
