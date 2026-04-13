import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import type { DayPlan } from '../../types/stride'

interface StridePlan {
  id: number
  week_start: string
  week_end: string
  phase: string
  plan: DayPlan[]
  model: string
  created_at: string
}

export default function StridePlanCard() {
  const { t, i18n } = useTranslation('stride')
  const { t: tToday } = useTranslation('today')
  const { user } = useAuth()
  const [plan, setPlan] = useState<StridePlan | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()

    fetch('/api/stride/plans/current', {
      credentials: 'include',
      signal: controller.signal,
    })
      .then(async (res) => {
        if (controller.signal.aborted) return
        if (res.status === 404) {
          setPlan(null)
          return
        }
        if (!res.ok) throw new Error('Failed to fetch plan')
        const data = await res.json()
        if (controller.signal.aborted) return
        setPlan(data.plan as StridePlan)
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(true)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => { controller.abort() }
  }, [user])

  const today = new Date().toISOString().slice(0, 10)
  const todayPlan = plan?.plan?.find((d) => d.date === today)

  const formatDate = (dateStr: string) =>
    new Intl.DateTimeFormat(i18n.language, { month: 'short', day: 'numeric' }).format(new Date(dateStr + 'T00:00:00'))

  const sessionsPlanned = plan?.plan?.filter((d) => !d.rest_day).length ?? 0

  return (
    <div className="bg-gray-800 rounded-xl p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xs uppercase tracking-wide text-gray-500">
          {t('title')}
        </h2>
        <Link to="/stride" className="text-xs text-gray-500 hover:text-gray-400" aria-label={tToday('viewMore')}>
          →
        </Link>
      </div>

      {loading && (
        <div className="space-y-3" role="status" aria-live="polite">
          <span className="sr-only">{t('loading')}</span>
          <div className="h-4 bg-gray-700 rounded animate-pulse w-3/4" />
          <div className="h-4 bg-gray-700 rounded animate-pulse w-1/2" />
        </div>
      )}

      {error && !loading && (
        <p className="text-red-400 text-sm">{t('plan.loadError')}</p>
      )}

      {!loading && !error && !plan && (
        <p className="text-gray-500 text-sm">{t('plan.empty')}</p>
      )}

      {!loading && !error && plan && (
        <div className="space-y-3">
          {/* Phase and week info */}
          <div className="flex items-center justify-between text-xs text-gray-400">
            <span className="capitalize">{plan.phase}</span>
            <span>{formatDate(plan.week_start)} – {formatDate(plan.week_end)}</span>
          </div>

          {/* Today's session */}
          {todayPlan ? (
            todayPlan.rest_day ? (
              <div className="text-sm text-gray-400">
                {t('plan.restDay')}
              </div>
            ) : todayPlan.session ? (
              <div className="space-y-1">
                <p className="text-sm text-gray-200">{todayPlan.session.description}</p>
                {todayPlan.session.target_hr_cap > 0 && (
                  <p className="text-xs text-gray-500">
                    {t('plan.targetHR')}: {t('plan.bpm', { value: todayPlan.session.target_hr_cap })}
                  </p>
                )}
              </div>
            ) : null
          ) : (
            <p className="text-xs text-gray-500">
              {t('plan.phase', { phase: plan.phase })} · {t('plan.sessionCount', { count: sessionsPlanned })}
            </p>
          )}

          {/* Week overview dots */}
          <div className="flex gap-1.5">
            {plan.plan?.map((day) => {
              const isToday = day.date === today
              const isPast = day.date < today
              const isRest = day.rest_day
              return (
                <div
                  key={day.date}
                  className={`h-2 flex-1 rounded-full ${
                    isRest
                      ? 'bg-gray-700'
                      : isToday
                        ? 'bg-blue-500'
                        : isPast
                          ? 'bg-gray-500'
                          : 'bg-gray-600'
                  }`}
                  title={`${formatDate(day.date)}${isRest ? ` – ${t('plan.restDay')}` : ''}`}
                />
              )
            })}
          </div>


        </div>
      )}
    </div>
  )
}
