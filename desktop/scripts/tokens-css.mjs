// Pure generator: design-system tokens.json -> tokens.css. Used by
// scripts/sync-tokens.mjs (writes the file) and by test/tokens.test.ts (parity).
// The first colour theme (dark) is the :root default; every other theme is a
// `:root[data-theme="<id>"]` block that redefines the colour tokens only
// (spacing, radius, size, shadow and type do not change with the theme).

const kebab = (s) => s.replace(/[^A-Za-z0-9_-]+/g, '-')

export function buildTokensCss(tokens) {
  const out = ['/* Generated from src/design/tokens.json by scripts/sync-tokens.mjs. Do not edit. */', ':root {']
  const [base, ...others] = tokens.color.themes.map((t) => t.id)
  for (const t of tokens.color.tokens) out.push(`  --${kebab(t.name)}: ${t.value[base]};`)
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
  for (const theme of others) {
    out.push(`:root[data-theme="${theme}"] {`, `  color-scheme: ${theme};`)
    for (const t of tokens.color.tokens) out.push(`  --${kebab(t.name)}: ${t.value[theme] ?? t.value[base]};`)
    out.push('}', '')
  }
  return out.join('\n')
}
