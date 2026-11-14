import { useI18n } from './i18n/context'
import { Button, StateMessage } from './ui/primitives'

export function LoadingState() {
  const { t } = useI18n()
  return <StateMessage kind="loading">{t('common.loading')}</StateMessage>
}

/** Shown instead of data when a read failed: stale data must not look current. */
export function EngineErrorState({ onRetry }: { onRetry: () => void }) {
  const { t } = useI18n()
  return (
    <StateMessage
      kind="error"
      action={
        <Button size="sm" onClick={onRetry} data-testid="retry">
          {t('state.retry')}
        </Button>
      }
    >
      <b>{t('state.errorTitle')}</b>
      <div className="mu">{t('state.errorBody')}</div>
    </StateMessage>
  )
}
