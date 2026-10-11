import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { useWidgetData } from './useWidgetData'
import { strideSource, todaysSession } from './sources'
import { toLocalDateString } from '../../utils/formatDate'

export default function StrideWidget() {
  const { t } = useTranslation('dashboard')
  const { t: tStride } = useTranslation('stride')
  const { data: plan, loaded } = useWidgetData(strideSource)
  if (!loaded || !plan) return null

  const today = toLocalDateString()
  const days = plan.plan ?? []
  const session = todaysSession(plan)

  return (
    <Widget title={t('widgets.stride.title')}>
      <p className="text-xs uppercase tracking-wide text-gray-500">{t('widgets.stride.today')}</p>
      {session ? (
        <>
          <p className="mt-1 text-sm text-gray-200">{session.description}</p>
          {session.target_hr_cap > 0 && (
            <p className="mt-0.5 text-xs text-gray-500">
              {tStride('plan.targetHR')}: {tStride('plan.bpm', { value: session.target_hr_cap })}
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
