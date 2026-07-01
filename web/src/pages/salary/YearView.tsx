import { useState } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ReferenceLine,
  ResponsiveContainer,
} from 'recharts'
import { shortMonthLabel } from './types'
import type { SalaryData } from './useSalaryData'

interface YearViewProps {
  salary: SalaryData
  selectedYear: number
  currentYear: number
  locale: string
  setSelectedYear: Dispatch<SetStateAction<number>>
}

/** Year tab: year selector, utilization chart and the monthly overview table. */
export default function YearView({ salary, selectedYear, currentYear, locale, setSelectedYear }: YearViewProps) {
  const { t } = useTranslation('salary')
  const { yearData, yearLoading, yearError, formatCurrency, confirmMonth, syncBudget } = salary

  const [confirming, setConfirming] = useState<string | null>(null)
  const [confirmError, setConfirmError] = useState<string | null>(null)
  const [syncing, setSyncing] = useState<string | null>(null)
  const [syncResults, setSyncResults] = useState<Record<string, string>>({})
  const [syncErrors, setSyncErrors] = useState<Record<string, string>>({})

  const handleConfirm = async (month: string) => {
    setConfirming(month)
    setConfirmError(null)
    try {
      await confirmMonth(month)
    } catch (err) {
      setConfirmError(err instanceof Error ? err.message : t('errors.failedToConfirm'))
    } finally {
      setConfirming(null)
    }
  }

  const handleSyncBudget = async (month: string) => {
    setSyncing(month)
    setSyncErrors(prev => { const n = { ...prev }; delete n[month]; return n })
    try {
      const data = await syncBudget(month)
      setSyncResults(prev => ({
        ...prev,
        [month]: t('budgetSync.synced', { amount: formatCurrency(data.net_income) }),
      }))
    } catch (err) {
      setSyncErrors(prev => ({
        ...prev,
        [month]: err instanceof Error ? err.message : t('budgetSync.error'),
      }))
    } finally {
      setSyncing(null)
    }
  }

  return (
    <div className="space-y-6">
      {/* Year selector */}
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => setSelectedYear(y => Math.max(2000, y - 1))}
          disabled={selectedYear <= 2000}
          aria-label={t('year.prev')}
          className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-700 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        >
          <ChevronLeft size={18} />
        </button>
        <span className="text-white font-semibold w-16 text-center">{selectedYear}</span>
        <button
          type="button"
          onClick={() => setSelectedYear(y => y + 1)}
          disabled={selectedYear >= currentYear}
          aria-label={t('year.next')}
          className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-700 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        >
          <ChevronRight size={18} />
        </button>
      </div>

      {yearLoading && <div className="text-gray-400 py-4">{t('year.title')}…</div>}
      {yearError && <div className="text-red-400">{t('errors.failedToLoad')}: {yearError}</div>}
      {confirmError && <div className="text-red-400 text-sm">{confirmError}</div>}

      {yearData && !yearLoading && (
        <>
          {/* Utilization chart */}
          <div className="bg-gray-800 rounded-xl p-5">
            <h2 className="text-base font-medium text-white mb-4">{t('year.chart.title')}</h2>
            <div role="img" aria-label={t('year.chart.title')}>
            <ResponsiveContainer width="100%" height={180}>
              <LineChart
                data={yearData.months.map(mp => ({
                  name: shortMonthLabel(mp.month, locale),
                  utilization: Math.round(mp.utilization_pct),
                }))}
                margin={{ top: 4, right: 8, left: -16, bottom: 0 }}
              >
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="name" tick={{ fill: '#9CA3AF', fontSize: 11 }} />
                <YAxis
                  tick={{ fill: '#9CA3AF', fontSize: 11 }}
                  domain={[0, 120]}
                  tickFormatter={v => `${v}%`}
                />
                <Tooltip
                  contentStyle={{ backgroundColor: '#1F2937', border: 'none', borderRadius: '8px' }}
                  labelStyle={{ color: '#F3F4F6' }}
                  itemStyle={{ color: '#60A5FA' }}
                  formatter={(value) => [`${typeof value === 'number' ? value : 0}%`, t('year.chart.utilization')]}
                />
                <ReferenceLine y={100} stroke="#6B7280" strokeDasharray="4 2" />
                <Line
                  type="monotone"
                  dataKey="utilization"
                  stroke="#3B82F6"
                  strokeWidth={2}
                  dot={{ r: 3, fill: '#3B82F6' }}
                  activeDot={{ r: 5 }}
                />
              </LineChart>
            </ResponsiveContainer>
            </div>
          </div>

          {/* Year overview table */}
          <div className="bg-gray-800 rounded-xl overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-700">
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 whitespace-nowrap">
                      {t('year.table.month')}
                    </th>
                    <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                      {t('year.table.days')}
                    </th>
                    <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                      {t('year.table.utilization')}
                    </th>
                    <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                      {t('year.table.gross')}
                    </th>
                    <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                      {t('year.table.tax')}
                    </th>
                    <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                      {t('year.table.net')}
                    </th>
                    <th className="px-3 py-3 text-center text-xs font-medium text-gray-400 whitespace-nowrap">
                      {t('year.table.status')}
                    </th>
                    <th className="px-3 py-3" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-700/50">
                  {yearData.months.map(mp => {
                    const rowClass = mp.is_current
                      ? 'bg-blue-950/30'
                      : mp.is_future
                      ? 'opacity-60'
                      : ''

                    const statusBadge = mp.is_estimate
                      ? mp.is_future
                        ? (
                          <span className="text-xs px-1.5 py-0.5 rounded bg-gray-700 text-gray-400">
                            {t('year.status.projected')}
                          </span>
                        )
                        : (
                          <span className="text-xs px-1.5 py-0.5 rounded bg-yellow-900/60 text-yellow-300">
                            {t('year.status.estimate')}
                          </span>
                        )
                      : (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-green-900/60 text-green-400">
                          {t('year.status.actual')}
                        </span>
                      )

                    // Allow confirming past estimate months (not current, not future, not already confirmed).
                    const canConfirm = mp.is_estimate && !mp.is_future && !mp.is_current
                    // Allow syncing confirmed past months (or any non-future month with net income).
                    const canSync = !mp.is_future && mp.net > 0

                    const utilColor = mp.utilization_pct >= 100
                      ? 'text-green-400'
                      : mp.utilization_pct >= 80
                      ? 'text-yellow-400'
                      : 'text-gray-400'

                    return (
                      <tr
                        key={mp.month}
                        className={`${rowClass} hover:bg-gray-700/30 transition-colors`}
                      >
                        <td className="px-4 py-2.5 text-gray-200 font-medium whitespace-nowrap">
                          {shortMonthLabel(mp.month, locale)}
                        </td>
                        <td className="px-3 py-2.5 text-right text-gray-400 tabular-nums">
                          {mp.working_days}
                        </td>
                        <td className={`px-3 py-2.5 text-right tabular-nums ${utilColor}`}>
                          {mp.utilization_pct.toFixed(0)}%
                        </td>
                        <td className="px-3 py-2.5 text-right text-gray-200 tabular-nums whitespace-nowrap">
                          {formatCurrency(mp.gross)}
                        </td>
                        <td className="px-3 py-2.5 text-right text-red-400 tabular-nums whitespace-nowrap">
                          {formatCurrency(mp.tax)}
                        </td>
                        <td className="px-3 py-2.5 text-right text-green-400 font-medium tabular-nums whitespace-nowrap">
                          {formatCurrency(mp.net)}
                        </td>
                        <td className="px-3 py-2.5 text-center whitespace-nowrap">
                          {statusBadge}
                        </td>
                        <td className="px-3 py-2.5 text-right whitespace-nowrap">
                          <div className="flex gap-1 justify-end">
                            {canConfirm && (
                              <button
                                type="button"
                                onClick={() => handleConfirm(mp.month)}
                                disabled={confirming === mp.month}
                                className="text-xs px-2 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 hover:text-white rounded transition-colors disabled:opacity-50"
                              >
                                {confirming === mp.month ? t('year.confirming') : t('year.confirm')}
                              </button>
                            )}
                            {canSync && (
                              <button
                                type="button"
                                onClick={() => handleSyncBudget(mp.month)}
                                disabled={syncing === mp.month}
                                title={syncResults[mp.month] ?? t('budgetSync.sync')}
                                className={`text-xs px-2 py-1 rounded transition-colors disabled:opacity-50 ${
                                  syncResults[mp.month]
                                    ? 'bg-green-900/60 text-green-400'
                                    : syncErrors[mp.month]
                                    ? 'bg-red-900/60 text-red-400 hover:bg-red-900/80'
                                    : 'bg-gray-700 hover:bg-gray-600 text-gray-300 hover:text-white'
                                }`}
                              >
                                {syncing === mp.month
                                  ? t('budgetSync.syncing')
                                  : syncResults[mp.month]
                                  ? '✓'
                                  : syncErrors[mp.month]
                                  ? '!'
                                  : '↔'}
                              </button>
                            )}
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
                <tfoot>
                  <tr className="border-t border-gray-600">
                    <td className="px-4 py-3 text-sm font-medium text-gray-300">
                      {t('year.totals')}
                    </td>
                    <td />
                    <td />
                    <td className="px-3 py-3 text-right text-sm font-semibold text-gray-200 tabular-nums whitespace-nowrap">
                      {formatCurrency(yearData.totals.gross)}
                    </td>
                    <td className="px-3 py-3 text-right text-sm font-semibold text-red-400 tabular-nums whitespace-nowrap">
                      {formatCurrency(yearData.totals.tax)}
                    </td>
                    <td className="px-3 py-3 text-right text-sm font-bold text-green-400 tabular-nums whitespace-nowrap">
                      {formatCurrency(yearData.totals.net)}
                    </td>
                    <td />
                    <td />
                  </tr>
                </tfoot>
              </table>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
