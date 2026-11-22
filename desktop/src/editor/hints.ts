import type { Diagnostic } from '../types'
import { locate } from './issues'
import type { ConnectionOption, Definition } from './types'

// "Needs a connection" (F2.3 requirement 7). The hint list derives only from two
// of the engine's diagnostic codes, the only file in the renderer that names a
// code (test/no-graph-rules.test.ts allows exactly this file); the candidates
// are the engine's own answers to list_connections.

const UNUSED_OUTPUT = 'W_DECLARED_OUTPUT_UNUSED'
const UNBOUND_INPUT = 'E_REQUIRED_INPUT_UNBOUND'

/** How many accepted candidates are shown before "show all". */
export const OPTIONS_SHOWN = 20

export interface Hint {
  kind: 'unused-output' | 'unbound-input'
  /** The output (`NODE.outputs.PORT`) or the input (`NODE.inputs.PORT`) the diagnostic names. */
  ref: string
  message: string
  key: string
}

/** One hint per matching diagnostic whose port still exists, in the engine's order. */
export function hintsOf(diags: Diagnostic[], def: Definition | undefined): Hint[] {
  return diags.flatMap((d, i): Hint[] => {
    if (d.code !== UNUSED_OUTPUT && d.code !== UNBOUND_INPUT) return []
    const { target } = locate(d, def)
    if (target.kind !== 'port') return []
    const ref = `${target.node}.${target.side}.${target.port}`
    return [{ kind: d.code === UNUSED_OUTPUT ? 'unused-output' : 'unbound-input', ref, message: d.message, key: `${i}:${ref}` }]
  })
}

/** The accepted candidates, required ports first, otherwise in the engine's order. */
export function acceptedOptions(options: ConnectionOption[]): ConnectionOption[] {
  const ok = options.filter((o) => o.ok)
  return [...ok.filter((o) => o.required), ...ok.filter((o) => !o.required)]
}

export function visibleOptions(options: ConnectionOption[], showAll: boolean): ConnectionOption[] {
  return showAll ? options : options.slice(0, OPTIONS_SHOWN)
}
