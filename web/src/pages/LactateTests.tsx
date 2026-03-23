import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth'
import { Activity, Plus, Calendar, ChevronRight, TrendingUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { LactateTest } from '../types/lactate'

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
      try {
        const res = await fetch('/api/lactate/tests', { credentials: 'include', signal: controller.signal })
        if (!res.ok) throw new Error('Failed to load tests')
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
  }, [user])

  if (!user) {
    return (
      <div className="p-6">
        <p className="text-gray-400">{t('signInToView')}</p>
      </div>
    )
  }

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

      {loading ? (
        <div className="text-center py-12 text-gray-400">{t('list.loading')}</div>
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
            const dateStr = new Date(y, m - 1, d).toLocaleDateString(undefined, {
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
                    <div className="flex items-center gap-4 mt-1.5 text-xs text-gray-500">
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
