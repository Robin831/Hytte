import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { formatHours } from './types'
import type { CommissionTier } from './types'
import { hoursToNextTier, maxExtraHours, projectFromExtraHours } from './tierMath'

interface WhatIfTierSliderProps {
  /** Absence-adjusted tiers — the same list the tier bars are drawn from. */
  tiers: CommissionTier[]
  /** Revenue the tier bars are drawn at (billable + internal). */
  baselineRevenue: number
  hourlyRate: number
  currentGross: number
  currentNet: number
  extraHourNet: number
  formatCurrency: (amount: number) => string
}

const STEP = 0.5

/**
 * Client-side "what if I work N more billable hours this month" control.
 *
 * Everything is recomputed from the already-fetched estimate — no network
 * request, no server-side what-if. Net is a linear extrapolation of the
 * server's per-extra-hour net figure (the backend may use a trekktabell lookup
 * table that the frontend never sees), so it is labelled as an estimate.
 */
export default function WhatIfTierSlider({
  tiers,
  baselineRevenue,
  hourlyRate,
  currentGross,
  currentNet,
  extraHourNet,
  formatCurrency,
}: WhatIfTierSliderProps) {
  const { t } = useTranslation('salary')
  const [extraHours, setExtraHours] = useState(0)
  const [inputText, setInputText] = useState('0')

  const maxHours = maxExtraHours(tiers, baselineRevenue, hourlyRate)

  // Pure tier arithmetic over a handful of tiers — cheap enough to redo on every
  // render, so no memoisation.
  const baseInput = { tiers, baselineRevenue, hourlyRate, currentGross, currentNet, extraHourNet }
  const current = projectFromExtraHours({ ...baseInput, extraHours: 0 })
  const projected = projectFromExtraHours({ ...baseInput, extraHours })

  const toNextTier = hoursToNextTier(tiers, baselineRevenue, hourlyRate)
  const nextTierNetGain =
    toNextTier === null
      ? 0
      : projectFromExtraHours({ ...baseInput, extraHours: toNextTier }).net - current.net

  const clamp = (value: number) => Math.min(Math.max(value, 0), maxHours)

  const applyHours = (value: number) => {
    const clamped = clamp(value)
    setExtraHours(clamped)
    setInputText(String(clamped))
  }

  const handleTextChange = (raw: string) => {
    setInputText(raw)
    const parsed = Number(raw.replace(',', '.'))
    if (raw.trim() !== '' && Number.isFinite(parsed)) {
      setExtraHours(clamp(parsed))
    }
  }

  const delta = (value: number) => t('commission.whatIf.delta', { amount: formatCurrency(value) })

  const rows: { key: string; label: string; value: string; diff: number }[] = [
    {
      key: 'revenue',
      label: t('commission.whatIf.revenue'),
      value: formatCurrency(projected.revenue),
      diff: projected.revenue - current.revenue,
    },
    {
      key: 'commission',
      label: t('commission.whatIf.commission'),
      value: formatCurrency(projected.commission),
      diff: projected.commission - current.commission,
    },
    {
      key: 'gross',
      label: t('commission.whatIf.gross'),
      value: formatCurrency(projected.gross),
      diff: projected.gross - current.gross,
    },
    {
      key: 'net',
      label: t('commission.whatIf.net'),
      value: formatCurrency(projected.net),
      diff: projected.net - current.net,
    },
  ]

  return (
    <div className="border-t border-gray-700 pt-4 space-y-3">
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-sm font-medium text-white">{t('commission.whatIf.title')}</h3>
        {extraHours > 0 && (
          <button
            type="button"
            onClick={() => applyHours(0)}
            className="text-xs text-gray-400 hover:text-white transition-colors"
          >
            {t('commission.whatIf.reset')}
          </button>
        )}
      </div>

      {toNextTier !== null && (
        <p className="text-sm text-blue-300">
          {t('commission.whatIf.hint', {
            hours: formatHours(toNextTier),
            amount: formatCurrency(nextTierNetGain),
          })}
        </p>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <label htmlFor="whatif-extra-hours" className="text-xs text-gray-400 shrink-0">
          {t('commission.whatIf.label')}
        </label>
        <input
          id="whatif-extra-hours"
          type="number"
          value={inputText}
          onChange={e => handleTextChange(e.target.value)}
          onBlur={() => setInputText(String(extraHours))}
          min={0}
          max={maxHours}
          step={STEP}
          className="w-20 bg-gray-700 text-white rounded-lg px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
        />
        <input
          type="range"
          value={extraHours}
          onChange={e => applyHours(Number(e.target.value))}
          min={0}
          max={maxHours}
          step={STEP}
          aria-label={t('commission.whatIf.sliderAria')}
          className="w-full accent-blue-500"
        />
      </div>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-2">
        {rows.map(row => (
          <div key={row.key} className="min-w-0">
            <dt className="text-xs text-gray-400 truncate">{row.label}</dt>
            <dd className="text-sm text-white">
              {row.value}
              {row.diff > 0.5 && (
                <span className="text-xs text-green-400 ml-1">{delta(row.diff)}</span>
              )}
            </dd>
          </div>
        ))}
      </dl>

      {/* Projected tier placement — same math as the bars above, at the projected revenue. */}
      <div className="space-y-1.5">
        {tiers.map((tier, idx) => {
          const p = projected.perTier[idx]
          const gained = p.earnings - current.perTier[idx].earnings
          return (
            <div key={tier.id} className="flex items-center gap-2 text-xs">
              <span className="text-gray-400 w-14 shrink-0">
                {t('commission.tier', { n: idx + 1 })}
              </span>
              <div className="flex-1 bg-gray-700 rounded-full h-1.5 min-w-0">
                <div
                  className="bg-blue-400 h-1.5 rounded-full transition-all"
                  style={{ width: `${p.progress}%` }}
                />
              </div>
              <span className="text-gray-300 tabular-nums shrink-0">
                {formatCurrency(p.earnings)}
              </span>
              {gained > 0.5 && <span className="text-green-400 shrink-0">{delta(gained)}</span>}
            </div>
          )
        })}
      </div>

      <p className="text-xs text-gray-500">{t('commission.whatIf.netNote')}</p>
    </div>
  )
}
