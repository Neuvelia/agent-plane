// node scripts/sync-tokens.mjs [--check]
// Regenerates src/design/tokens.css from src/design/tokens.json. To update the
// tokens, replace tokens.json with the design system's project/tokens.json and
// rerun. --check exits 1 if tokens.css is stale (used by npm run check).
import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { buildTokensCss } from './tokens-css.mjs'

const dir = fileURLToPath(new URL('../src/design/', import.meta.url))
const css = buildTokensCss(JSON.parse(readFileSync(dir + 'tokens.json', 'utf8')))
if (process.argv.includes('--check')) {
  let current = ''
  try { current = readFileSync(dir + 'tokens.css', 'utf8') } catch { /* missing */ }
  if (current !== css) { console.error('tokens.css is stale: run node scripts/sync-tokens.mjs'); process.exit(1) }
} else {
  writeFileSync(dir + 'tokens.css', css)
}
