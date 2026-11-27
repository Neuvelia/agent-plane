import { useI18n } from '../i18n/context'
import { KindGlyph } from './glyphs'
import { NODE_TYPES } from './layout'
import { MOD } from './Menu'

// The canvas of a graph with no steps (graph editor v2): the first step, by kind,
// and the instructions that used to sit above the canvas.
export function EmptyGraph({ onAdd, onYaml, disabled }: { onAdd: (type: string) => void; onYaml: () => void; disabled: boolean }) {
  const { t } = useI18n()
  return (
    <div className="er-empty">
      <div className="er-ecard" data-testid="empty-graph">
        <div className="er-ecard-head">
          <h2>{t('editor.v2.emptyTitle')}</h2>
          <p>{t('editor.v2.emptyText')}</p>
        </div>
        <div className="er-types">
          {NODE_TYPES.map((type) => (
            <button key={type} className={`er-type k-${type}`} type="button" onClick={() => onAdd(type)} disabled={disabled} data-testid={`empty-add-${type}`}>
              <KindGlyph type={type} size={14} />
              <div>
                <b>{t(`editor.type.${type}`)}</b>
                <span>{t(`editor.v2.typeHint.${type}`)}</span>
              </div>
            </button>
          ))}
        </div>
        <div>
          <button className="btn ghost" type="button" style={{ padding: '0 8px', marginLeft: -8 }} onClick={onYaml} data-testid="empty-yaml">
            {t('editor.v2.emptyYaml')}
          </button>
        </div>
        <ul className="er-tips">
          <li>{t('editor.v2.tipConnect')}</li>
          <li>{t('editor.v2.tipSource', { undo: `${MOD}Z` })}</li>
        </ul>
      </div>
    </div>
  )
}
