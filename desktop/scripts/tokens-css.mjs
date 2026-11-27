// Pure generator: design-system tokens.json -> tokens.css. Used by
// scripts/sync-tokens.mjs (writes the file) and by test/tokens.test.ts (parity).
// The first theme (dark) is the default on :root. Every other theme overrides
// only the colour tokens under :root[data-theme="<id>"] (F2.5b, ADR-0046).

const kebab = (s) => s.replace(/[^A-Za-z0-9_-]+/g, '-')

function colours(tokens, theme) {
  return tokens.color.tokens.filter((t) => t.value[theme] !== undefined).map((t) => `  --${kebab(t.name)}: ${t.value[theme]};`)
}

export function buildTokensCss(tokens) {
  const [base, ...others] = tokens.color.themes.map((t) => t.id)
  const out = ['/* Generated from src/design/tokens.json by scripts/sync-tokens.mjs. Do not edit. */', ':root {']
  out.push(...colours(tokens, base))
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
  out.push('}')
  for (const theme of others) out.push(`:root[data-theme="${theme}"] {`, ...colours(tokens, theme), '}')
  out.push('')
  return out.join('\n')
}
