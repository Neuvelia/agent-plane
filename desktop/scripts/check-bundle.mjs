// Fails if the production renderer bundle (dist/) contains Storybook code or
// references any remote origin. Run after `npm run build`.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) yield* walk(p)
    else yield p
  }
}

const bad = []
for (const file of walk(new URL('../dist', import.meta.url).pathname)) {
  if (!/\.(js|css|html)$/.test(file)) continue
  const text = readFileSync(file, 'utf8')
  if (/storybook/i.test(text)) bad.push(`${file}: mentions storybook`)
  // The CSP in index.html legitimately names no origin; any http(s) URL in
  // css/html is a remote load (JS strings such as XML namespaces are ignored).
  if (/\.(css|html)$/.test(file) && /(?:src|href|url)\s*[=(]\s*["']?https?:\/\//i.test(text)) bad.push(`${file}: remote URL`)
}
if (bad.length) {
  console.error(bad.join('\n'))
  process.exit(1)
}
console.log('bundle ok: no Storybook code, no remote URLs')
