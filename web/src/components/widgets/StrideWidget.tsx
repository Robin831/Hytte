import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { useWidgetData } from './useWidgetData'
import { toLocalDateString } from '../../utils/formatDate'
import type { DayPlan } from '../../types/stride'

interface StridePlan {
  id: number
  week_start: string
  week_end: string
  phase: string
  plan: DayPlan[] | null
}

/** A week without a plan answers 404, which is "nothing to show", not a failure. */
async function loadPlan(signal: AbortSignal): Promise<StridePlan | null> {
  const res = await fetch('/api/stride/plans/current', { credentials: 'include', signal })
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`stride plan: ${res.status}`)
  const data = (await res.json()) as { plan: StridePlan | null }
  return data.plan
}

export default function StrideWidget() {
  const { t } = useTranslation('dashboard')
  const { t: tStride } = useTranslation('stride')
  const { data: plan, loaded } = useWidgetData(loadPlan)
  if (!loaded || !plan) return null

  const today = toLocalDateString()
  const days = plan.plan ?? []
  const todayPlan = days.find((d) => d.date === today)

  return (
    <Widget title={t('widgets.stride.title')}>
      <p className="text-xs uppercase tracking-wide text-gray-500">{t('widgets.stride.today')}</p>
      {todayPlan?.session && !todayPlan.rest_day ? (
        <>
          <p className="mt-1 text-sm text-gray-200">{todayPlan.session.description}</p>
          {todayPlan.session.target_hr_cap > 0 && (
            <p className="mt-0.5 text-xs text-gray-500">
              {tStride('plan.targetHR')}: {tStride('plan.bpm', { value: todayPlan.session.target_hr_cap })}
            </p>
          )}
        </>
      ) : (
        <p className="mt-1 text-sm text-gray-400">{tStride('plan.restDay')}</p>
      )}

      {/* The week at a glance: one bar per day, today highlighted. */}
      <div className="mt-4 flex gap-1.5" aria-hidden="true">
        {days.map((day) => (
          <div
            key={day.date}
            className={`h-2 flex-1 rounded-full ${
              day.rest_day ? 'bg-gray-700' : day.date === today ? 'bg-blue-500' : day.date < today ? 'bg-gray-500' : 'bg-gray-600'
            }`}
          />
        ))}
      </div>
      <p className="mt-2 text-xs text-gray-500">
        {t('widgets.stride.sessions', { count: days.filter((d) => !d.rest_day).length })} · <span className="capitalize">{plan.phase}</span>
      </p>

      <WidgetLink to="/training/stride">{t('widgets.stride.open')}</WidgetLink>
    </Widget>
  )
}
