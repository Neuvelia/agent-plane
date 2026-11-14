// Pure generator: design-system tokens.json -> tokens.css. Used by
// scripts/sync-tokens.mjs (writes the file) and by test/tokens.test.ts (parity).
// Only the dark theme exists (F2.5a); other themes are ignored.

const kebab = (s) => s.replace(/[^A-Za-z0-9_-]+/g, '-')

export function buildTokensCss(tokens) {
  const out = ['/* Generated from src/design/tokens.json by scripts/sync-tokens.mjs. Do not edit. */', ':root {']
  const theme = tokens.color.themes[0].id
  for (const t of tokens.color.tokens) out.push(`  --${kebab(t.name)}: ${t.value[theme]};`)
  for (const family of ['spacing', 'radius', 'shadow', 'size']) {
    for (const t of tokens[family]?.tokens ?? []) out.push(`  --${kebab(t.name)}: ${t.value};`)
  }
  for (const [name, stack] of Object.entries(tokens.type.families)) out.push(`  --font-${kebab(name)}: ${stack};`)
  for (const g of tokens.type.groups) {
    for (const s of g.styles) {
      const p = `--type-${kebab(s.name)}`
      out.push(`  ${p}-size: ${s.fontSize};`, `  ${p}-line: ${s.lineHeight};`, `  ${p}-weight: ${s.fontWeight};`)
      if (s.letterSpacing) out.push(`  ${p}-tracking: ${s.letterSpacing};`)
    }
  }
  out.push('}', '')
  return out.join('\n')
}
