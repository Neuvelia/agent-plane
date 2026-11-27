import { useI18n } from '../i18n/context'
import { MOD, Menu, MenuItem, MenuSep, type Anchor } from './Menu'

// The step menu (graph editor v2), on a card's "…" button and on right-click: run
// actions first, then edit actions with their shortcuts, Delete last. Running one
// step and copying or duplicating a step have no engine support yet, so they are
// shown as "Soon" and do nothing.

export interface StepMenuAnchor {
  id: string
  anchor: Anchor
}

interface Props {
  open: StepMenuAnchor | null
  onClose: () => void
  readOnly: boolean
  hasOutputs: boolean
  onConnect: () => void
  onRename: () => void
  onDelete: () => void
}

export function StepMenu({ open, onClose, readOnly, hasOutputs, onConnect, onRename, onDelete }: Props) {
  const { t } = useI18n()
  const act = (f: () => void) => () => {
    onClose()
    f()
  }
  const close = (o?: { refocus: boolean }) => {
    if (o?.refocus && open?.anchor.kind === 'element') open.anchor.el.focus()
    onClose()
  }
  return (
    <Menu open={open !== null} anchor={open?.anchor ?? null} onClose={close} label={t('editor.v2.stepMenu', { id: open?.id ?? '' })} width={272} id="v2-stepmenu" testId="step-menu">
      <MenuItem icon="play" label={t('editor.v2.testStep')} soon testId="op-test" />
      <MenuItem icon="runFrom" label={t('editor.v2.runFrom')} soon testId="op-run-from" />
      <MenuSep />
      <MenuItem icon="connect" label={t('editor.connect')} disabled={readOnly || !hasOutputs} onSelect={act(onConnect)} testId="op-connect" />
      <MenuItem icon="pencil" label={t('editor.v2.rename')} keys={['F2']} disabled={readOnly} onSelect={act(onRename)} testId="op-rename" />
      <MenuItem icon="copy" label={t('editor.v2.copy')} keys={[MOD, 'C']} soon testId="op-copy" />
      <MenuItem icon="copy" label={t('editor.v2.duplicate')} keys={[MOD, 'D']} soon testId="op-duplicate" />
      <MenuSep />
      <MenuItem icon="trash" label={t('editor.v2.deleteStep')} keys={['⌫']} danger disabled={readOnly} onSelect={act(onDelete)} testId="op-delete" />
    </Menu>
  )
}
