import { useEffect, useRef, useState } from 'react'

// A text control that commits, not types (F2.2 requirement 8): the draft is
// local until blur or Enter (Ctrl/Cmd+Enter in a text area); Escape restores
// the file's value. A commit of unchanged text sends nothing.

interface Props {
  value: string
  onCommit: (text: string) => void
  id?: string
  multiline?: boolean
  /** A new-entry row: after a commit the draft returns to `value` for the next entry. */
  keepDraft?: boolean
  disabled?: boolean
  readOnly?: boolean
  label?: string
  describedBy?: string
  invalid?: boolean
  required?: boolean
  numeric?: boolean
  placeholder?: string
  autoFocus?: boolean
  className?: string
  testId?: string
}

export function TextCommit({ value, onCommit, id, multiline = false, keepDraft = false, disabled, readOnly, label, describedBy, invalid, required, numeric, placeholder, autoFocus, className = '', testId }: Props) {
  const [draft, setDraft] = useState(value)
  const committed = useRef(value)
  useEffect(() => {
    committed.current = value
    setDraft(value)
  }, [value])
  const commit = () => {
    if (draft === committed.current) return
    onCommit(draft)
    if (keepDraft) {
      committed.current = value
      setDraft(value)
    } else committed.current = draft
  }
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      setDraft(committed.current)
      return
    }
    if (e.key === 'Enter' && (!multiline || e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      commit()
    }
  }
  const common = {
    id,
    disabled,
    readOnly,
    placeholder,
    autoFocus,
    'aria-label': label,
    'aria-describedby': describedBy || undefined,
    'aria-invalid': invalid || undefined,
    'aria-required': required || undefined,
    value: draft,
    onBlur: commit,
    onKeyDown,
    'data-testid': testId,
  }
  if (multiline) return <textarea {...common} className={className} onChange={(e) => setDraft(e.target.value)} />
  return <input {...common} className={`input ${className}`.trim()} inputMode={numeric ? 'numeric' : undefined} onChange={(e) => setDraft(e.target.value)} />
}
