import { useState } from 'react'
import { Brain, AlertTriangle, CheckCircle, ChevronRight, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import type { SummaryAnalysisResponse } from '../types/training'
import { formatDate } from '../utils/formatDate'

export function WeeklyAiSummary() {
  const { user } = useAuth()
  const { t } = useTranslation('training')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<SummaryAnalysisResponse | null>(null)

  if (!user?.is_admin) return null

  const generate = async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/api/training/summary/analyze', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ period: 'week' }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setError((data as { error?: string }).error || t('errors.failedToAnalyzeSummary'))
        return
      }
      const data: SummaryAnalysisResponse = await res.json()
      setResult(data)
    } catch {
      setError(t('errors.failedToAnalyzeSummary'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="bg-gray-800 rounded-xl p-6 mb-6">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <Brain size={20} className="text-purple-400" />
          <h2 className="text-lg font-semibold">{t('trends.weeklySummary.title')}</h2>
        </div>
        <button
          type="button"
          onClick={generate}
          disabled={loading}
          className="flex items-center gap-2 px-3 py-1.5 text-sm bg-purple-600 hover:bg-purple-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg transition-colors"
        >
          {loading ? (
            <>
              <RefreshCw size={14} className="animate-spin" />
              {t('trends.weeklySummary.generating')}
            </>
          ) : result ? (
            t('trends.weeklySummary.regenerate')
          ) : (
            t('trends.weeklySummary.generate')
          )}
        </button>
      </div>

      {error && (
        <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm mb-4">
          {error}
        </div>
      )}

      {result && (
        <div className="space-y-4">
          {result.analysis.risk_flags.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {result.analysis.risk_flags.map((flag, i) => (
                <span
                  key={`${i}-${flag}`}
                  className="inline-flex items-center gap-1 px-2.5 py-1 bg-red-500/15 border border-red-500/30 text-red-400 text-xs rounded-full"
                >
                  <AlertTriangle size={12} />
                  {flag}
                </span>
              ))}
            </div>
          )}

          {result.analysis.overview && (
            <p className="text-gray-300 text-sm leading-relaxed">{result.analysis.overview}</p>
          )}

          {result.analysis.key_insights.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-400 mb-2">{t('trends.weeklySummary.keyInsights')}</h3>
              <ul className="space-y-1">
                {result.analysis.key_insights.map((insight, i) => (
                  <li key={`${i}-${insight}`} className="flex items-start gap-2 text-sm text-gray-300">
                    <ChevronRight size={14} className="mt-0.5 text-purple-400 shrink-0" />
                    {insight}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {result.analysis.strengths.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-400 mb-2">{t('trends.weeklySummary.strengths')}</h3>
              <div className="flex flex-wrap gap-2">
                {result.analysis.strengths.map((strength, i) => (
                  <span
                    key={`${i}-${strength}`}
                    className="inline-flex items-center gap-1 px-2.5 py-1 bg-green-500/15 border border-green-500/30 text-green-400 text-xs rounded-full"
                  >
                    <CheckCircle size={12} />
                    {strength}
                  </span>
                ))}
              </div>
            </div>
          )}

          {result.analysis.concerns.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-400 mb-2">{t('trends.weeklySummary.concerns')}</h3>
              <ul className="space-y-1">
                {result.analysis.concerns.map((concern, i) => (
                  <li key={`${i}-${concern}`} className="flex items-start gap-2 text-sm text-gray-300">
                    <AlertTriangle size={14} className="mt-0.5 text-yellow-400 shrink-0" />
                    {concern}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {result.analysis.recommendations.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-400 mb-2">{t('trends.weeklySummary.recommendations')}</h3>
              <ul className="space-y-1">
                {result.analysis.recommendations.map((rec, i) => (
                  <li key={`${i}-${rec}`} className="flex items-start gap-2 text-sm text-gray-300">
                    <ChevronRight size={14} className="mt-0.5 text-blue-400 shrink-0" />
                    {rec}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <p className="text-xs text-gray-500 pt-1">
            {t('trends.weeklySummary.analyzedBy', {
              model: result.model,
              date: formatDate(result.created_at, { dateStyle: 'medium' }),
            })}
            {result.cached && <span className="ml-1">{t('trends.weeklySummary.cached')}</span>}
          </p>
        </div>
      )}
    </div>
  )
}
