import { useTranslation } from 'react-i18next'
import { Zap } from 'lucide-react'
import './speedCallout.css'

export const FAST_THRESHOLD_MS = 2000
export const LIGHTNING_THRESHOLD_MS = 1000

export interface SpeedCalloutProps {
  responseMs: number | null
}

export function SpeedCallout({ responseMs }: SpeedCalloutProps) {
  const { t } = useTranslation('regnemester')
  if (responseMs === null) return null
  if (responseMs >= FAST_THRESHOLD_MS) return null

  const isLightning = responseMs < LIGHTNING_THRESHOLD_MS
  const variantClass = isLightning
    ? 'regnemester-speed-callout-lightning'
    : 'regnemester-speed-callout-fast'
  const label = isLightning ? t('speed.lightning') : t('speed.fast')

  return (
    <div
      className={`regnemester-speed-callout ${variantClass}`}
      role="status"
      aria-live="polite"
    >
      <Zap size={16} aria-hidden="true" />
      <span>{label}</span>
    </div>
  )
}
