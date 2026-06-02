import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth'
import { Activity, Plus, Calendar, ChevronRight, TrendingUp, ArrowUp, ArrowDown, Minus, Gauge } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDate, formatNumber } from '../utils/formatDate'
import type { LactateTest, PrimaryThreshold } from '../types/lactate'
import { Skeleton } from '../components/ui/skeleton'

function validThreshold(test: LactateTest | undefined): PrimaryThreshold | null {
  const pt = test?.primary_threshold
  return pt && pt.valid ? pt : null
}

// DeltaBadge renders a signed change with up/down/flat styling.
function DeltaBadge({ value, unit, decimals }: { value: number; unit: string; decimals: number }) {
  const { t } = useTranslation(['lactate'])
  const rounded = Number(value.toFixed(decimals))
  const magnitude = formatNumber(Math.abs(rounded), {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  })
  let Icon = Minus
  let color = 'text-gray-400'
  let sign = ''
  if (rounded > 0) {
    Icon = ArrowUp
    color = 'text-green-400'
    sign = '+'
  } else if (rounded < 0) {
    Icon = ArrowDown
    color = 'text-red-400'
    sign = '−' // minus sign
  }
  return (
    <span className={`inline-flex items-center gap-1 ${color}`}>
      <Icon size={14} className="shrink-0" />
      <span className="tabular-nums">
        {t('summary.delta', { sign: rounded === 0 ? '' : sign, value: magnitude, unit })}
      </span>
    </span>
  )
}

export default function LactateTests() {
  const { user } = useAuth()
  const { t } = useTranslation(['lactate', 'common'])
  const [tests, setTests] = useState<LactateTest[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    const load = async () => {
      setLoading(true)
      setError('')
      try {
        const res = await fetch('/api/lactate/tests', { credentials: 'include', signal: controller.signal })
        if (!res.ok) {
          setError(t('errors.failedToLoadLactateTests'))
          return
        }
        const data = await res.json()
        setTests(data.tests || [])
      } catch (err: unknown) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(t('errors.failedToLoadLactateTests'))
        }
      } finally {
        setLoading(false)
      }
    }
    load()
    return () => controller.abort()
  }, [user, t])

  return (
    <div className="max-w-4xl mx-auto p-4 md:p-6">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <Activity size={24} className="text-blue-400" />
          <h1 className="text-2xl font-bold">{t('title')}</h1>
        </div>
        <div className="flex items-center gap-2">
          {tests.length >= 2 && (
            <Link
              to="/lactate/insights"
              className="flex items-center gap-2 px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm font-medium transition-colors"
            >
              <TrendingUp size={16} />
              {t('list.insights')}
            </Link>
          )}
          <Link
            to="/lactate/new"
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium transition-colors"
          >
            <Plus size={16} />
            {t('list.newTest')}
          </Link>
        </div>
      </div>

      {error && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 mb-6 text-red-400">
          {error}
        </div>
      )}

      {!loading && tests.length >= 2 && (() => {
        const latest = validThreshold(tests[0])
        const previous = validThreshold(tests[1])
        return (
          <div className="bg-gray-800 border border-gray-700 rounded-xl p-4 mb-6">
            <div className="flex items-center gap-2 mb-3">
              <Gauge size={16} className="text-blue-400 shrink-0" />
              <h2 className="text-sm font-semibold text-gray-300">{t('summary.latestThreshold')}</h2>
            </div>
            {latest ? (
              <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
                <span className="text-2xl font-bold text-white tabular-nums">
                  {t('summary.value', {
                    speed: formatNumber(latest.speed_kmh, { minimumFractionDigits: 1, maximumFractionDigits: 1 }),
                    hr: latest.heart_rate_bpm,
                  })}
                </span>
                <span className="text-xs text-gray-500">{latest.method}</span>
              </div>
            ) : (
              <p className="text-gray-400 text-sm">{t('summary.noResultYet')}</p>
            )}
            {latest && previous ? (
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 mt-2 text-sm">
                <DeltaBadge value={latest.speed_kmh - previous.speed_kmh} unit={t('units.kmh')} decimals={1} />
                <DeltaBadge value={latest.heart_rate_bpm - previous.heart_rate_bpm} unit={t('units.bpm')} decimals={0} />
                <span className="text-xs text-gray-500">{t('summary.vsPrevious')}</span>
              </div>
            ) : (
              latest && <p className="text-xs text-gray-500 mt-2">{t('summary.noPreviousComparison')}</p>
            )}
          </div>
        )
      })()}

      {loading ? (
        <div className="space-y-3 py-4" role="status" aria-live="polite" aria-busy="true">
          <p className="sr-only">{t('list.loading')}</p>
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      ) : tests.length === 0 ? (
        <div className="bg-gray-800 rounded-xl p-8 text-center">
          <Activity size={40} className="text-gray-600 mx-auto mb-3" />
          <p className="text-gray-400 mb-4">{t('list.empty')}</p>
          <Link
            to="/lactate/new"
            className="inline-flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium transition-colors"
          >
            <Plus size={16} />
            {t('list.recordTest')}
          </Link>
        </div>
      ) : (
        <div className="space-y-3">
          {tests.map((test) => {
            const [y, m, d] = test.date.split('-').map(Number)
            const dateStr = formatDate(new Date(y, m - 1, d), {
              year: 'numeric', month: 'long', day: 'numeric',
            })
            return (
              <Link
                key={test.id}
                to={`/lactate/${test.id}`}
                className="block bg-gray-800 hover:bg-gray-700 border border-gray-700 hover:border-gray-600 rounded-xl p-4 transition-colors group"
              >
                <div className="flex items-center justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <Calendar size={14} className="text-gray-500 shrink-0" />
                      <span className="text-sm text-gray-400">{dateStr}</span>
                      <span className="text-xs bg-gray-700 text-gray-400 px-2 py-0.5 rounded-full">
                        {test.protocol_type}
                      </span>
                    </div>
                    {test.comment && (
                      <p className="text-white font-medium truncate">{test.comment}</p>
                    )}
                    <div className="mt-2">
                      {(() => {
                        const pt = validThreshold(test)
                        if (!pt) {
                          return (
                            <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full bg-gray-700/60 text-gray-400 text-xs">
                              <Gauge size={12} className="shrink-0" />
                              {t('summary.noResult')}
                            </span>
                          )
                        }
                        return (
                          <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full bg-blue-500/10 text-blue-300 text-xs font-medium">
                            <Gauge size={12} className="shrink-0" />
                            {t('summary.chip', {
                              method: pt.method,
                              speed: formatNumber(pt.speed_kmh, { minimumFractionDigits: 1, maximumFractionDigits: 1 }),
                              hr: pt.heart_rate_bpm,
                            })}
                          </span>
                        )
                      })()}
                    </div>
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 mt-1.5 text-xs text-gray-500">
                      <span>{t('list.startSpeed', { speed: test.start_speed_kmh })}</span>
                      <span>{t('list.speedSteps', { increment: test.speed_increment_kmh })}</span>
                      <span>{t('list.stageDuration', { duration: test.stage_duration_min })}</span>
                    </div>
                  </div>
                  <ChevronRight size={18} className="text-gray-600 group-hover:text-gray-400 shrink-0 ml-3 transition-colors" />
                </div>
              </Link>
            )
          })}
        </div>
      )}
    </div>
  )
}
