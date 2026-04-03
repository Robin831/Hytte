import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, TrendingUp, PieChart as PieChartIcon, BarChart2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  BarChart,
  Bar,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts'

interface CategoryTrend {
  category_id: number
  category_name: string
  color: string
  is_income: boolean
  amount: number
}

interface MonthlyTrend {
  month: string
  income: number
  expenses: number
  net: number
  by_category: CategoryTrend[]
}

interface NetWorthPoint {
  month: string
  value: number
}

interface YoYMonth {
  month: number
  current: number
  previous: number
}

interface YearOverYear {
  current_year: number
  previous_year: number
  monthly: YoYMonth[]
}

interface TrendsResponse {
  months: MonthlyTrend[]
  net_worth: NetWorthPoint[]
  year_over_year: YearOverYear | null
}

const DEFAULT_COLORS = [
  '#3b82f6', '#22c55e', '#f97316', '#a855f7', '#ec4899',
  '#14b8a6', '#eab308', '#6366f1', '#ef4444', '#84cc16',
]

function fmt(n: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(n)
}

// Format a YYYY-MM string as a locale-aware short month label.
// When showYear is true, appends a 2-digit year to disambiguate labels across calendar years.
function formatMonthLabel(yyyyMM: string, showYear: boolean): string {
  const parts = yyyyMM.split('-')
  if (parts.length < 2) return yyyyMM
  const year = parseInt(parts[0], 10)
  const month = parseInt(parts[1], 10)
  if (isNaN(year) || isNaN(month)) return yyyyMM
  const date = new Date(year, month - 1, 1)
  if (showYear) {
    return new Intl.DateTimeFormat(undefined, { month: 'short', year: '2-digit' }).format(date)
  }
  return new Intl.DateTimeFormat(undefined, { month: 'short' }).format(date)
}

// Format a calendar month number (1-12) as a locale-aware short month name.
function formatMonthNum(monthNum: number): string {
  return new Intl.DateTimeFormat(undefined, { month: 'short' }).format(new Date(2000, monthNum - 1, 1))
}

const TOOLTIP_STYLE = {
  contentStyle: {
    backgroundColor: '#1f2937',
    border: '1px solid #374151',
    borderRadius: '8px',
    color: '#e5e7eb',
  },
}

export default function BudgetCharts() {
  const { t } = useTranslation('budget')
  const [months, setMonths] = useState(6)
  const [data, setData] = useState<TrendsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    setLoading(true)
    setError(null)
    fetch(`/api/budget/trends?months=${months}`, { credentials: 'include', signal: controller.signal })
      .then(r => {
        if (!r.ok) throw new Error('fetch failed')
        return r.json() as Promise<TrendsResponse>
      })
      .then(setData)
      .catch(err => {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(t('charts.errors.loadFailed'))
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [months, t])

  // Current month pie chart data (expenses only, from last month in trends).
  const currentMonthData = data?.months[data.months.length - 1]
  const pieData = currentMonthData?.by_category
    .filter(c => !c.is_income && c.amount < 0)
    .map(c => ({
      name: c.category_name || t('noCategory'),
      value: Math.abs(c.amount),
      color: c.color || DEFAULT_COLORS[0],
    })) ?? []

  // Show the year in X-axis labels when the range spans more than one calendar year.
  const showYear = months > 12

  // Bar chart: monthly income vs expenses.
  const barData = data?.months.map(m => ({
    month: formatMonthLabel(m.month, showYear),
    income: m.income,
    expenses: m.expenses,
  })) ?? []

  // Line chart: net worth over time.
  const netWorthData = data?.net_worth.map(p => ({
    month: formatMonthLabel(p.month, showYear),
    value: p.value,
  })) ?? []

  // Year-over-year bar chart.
  const yoy = data?.year_over_year
  const yoyData = yoy?.monthly.map(m => ({
    month: formatMonthNum(m.month),
    current: m.current,
    previous: m.previous,
  })) ?? []

  return (
    <div className="max-w-5xl mx-auto p-4 md:p-6">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Link
          to="/budget"
          className="p-2 rounded-lg bg-gray-800 hover:bg-gray-700 transition-colors"
          aria-label={t('charts.back')}
        >
          <ArrowLeft size={18} />
        </Link>
        <TrendingUp size={22} className="text-blue-400" />
        <h1 className="text-xl font-bold">{t('charts.title')}</h1>

        {/* Month selector */}
        <div className="ml-auto flex items-center gap-2">
          <label htmlFor="months-select" className="text-sm text-gray-400">
            {t('charts.showMonths')}
          </label>
          <select
            id="months-select"
            value={months}
            onChange={e => setMonths(Number(e.target.value))}
            className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm"
          >
            {[3, 6, 12, 24].map(n => (
              <option key={n} value={n}>{n}</option>
            ))}
          </select>
        </div>
      </div>

      {loading && (
        <p className="text-gray-400 text-center py-12">{t('charts.loading')}</p>
      )}
      {error && (
        <p className="text-red-400 text-center py-12">{error}</p>
      )}

      {!loading && !error && data && (
        <div className="space-y-6">

          {/* Cash flow summary row */}
          {currentMonthData && (
            <div className="grid grid-cols-3 gap-4">
              <div className="bg-gray-800 rounded-xl p-4 text-center">
                <div className="text-xs text-gray-400 mb-1">{t('summary.income')}</div>
                <div className="text-xl font-bold text-green-400">{fmt(currentMonthData.income)}</div>
              </div>
              <div className="bg-gray-800 rounded-xl p-4 text-center">
                <div className="text-xs text-gray-400 mb-1">{t('summary.expenses')}</div>
                <div className="text-xl font-bold text-red-400">{fmt(currentMonthData.expenses)}</div>
              </div>
              <div className="bg-gray-800 rounded-xl p-4 text-center">
                <div className="text-xs text-gray-400 mb-1">{t('summary.net')}</div>
                <div className={`text-xl font-bold ${currentMonthData.net >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                  {fmt(currentMonthData.net)}
                </div>
              </div>
            </div>
          )}

          {/* Top row: pie + bar */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">

            {/* Pie chart: spending by category */}
            <div className="bg-gray-800 rounded-xl p-6">
              <div className="flex items-center gap-2 mb-4">
                <PieChartIcon size={16} className="text-blue-400" />
                <h2 className="font-semibold">{t('charts.spendingByCategory')}</h2>
              </div>
              {pieData.length === 0 ? (
                <p className="text-gray-500 text-sm text-center py-8">{t('charts.noData')}</p>
              ) : (
                <div className="w-full h-56">
                  <ResponsiveContainer width="100%" height="100%">
                    <PieChart>
                      <Pie
                        data={pieData}
                        dataKey="value"
                        nameKey="name"
                        cx="50%"
                        cy="50%"
                        outerRadius={80}
                        label={({ name, percent }) =>
                          `${name} ${((percent ?? 0) * 100).toFixed(0)}%`
                        }
                        labelLine={false}
                      >
                        {pieData.map((entry, index) => (
                          <Cell
                            key={`${entry.name}-${index}`}
                            fill={entry.color || DEFAULT_COLORS[index % DEFAULT_COLORS.length]}
                          />
                        ))}
                      </Pie>
                      <Tooltip
                        {...TOOLTIP_STYLE}
                        formatter={(value) => [fmt(Number(value)), ''] as [string, string]}
                      />
                    </PieChart>
                  </ResponsiveContainer>
                </div>
              )}
            </div>

            {/* Bar chart: monthly income vs expenses */}
            <div className="bg-gray-800 rounded-xl p-6">
              <div className="flex items-center gap-2 mb-4">
                <BarChart2 size={16} className="text-blue-400" />
                <h2 className="font-semibold">{t('charts.monthlySpending')}</h2>
              </div>
              {barData.length === 0 ? (
                <p className="text-gray-500 text-sm text-center py-8">{t('charts.noData')}</p>
              ) : (
                <div className="w-full h-56">
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart data={barData} margin={{ top: 5, right: 10, left: 0, bottom: 5 }}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                      <XAxis dataKey="month" tick={{ fill: '#9ca3af', fontSize: 11 }} />
                      <YAxis tick={{ fill: '#9ca3af', fontSize: 10 }} tickFormatter={fmt} />
                      <Tooltip
                        {...TOOLTIP_STYLE}
                        formatter={(value) => [fmt(Number(value)), ''] as [string, string]}
                      />
                      <Legend wrapperStyle={{ fontSize: 12, color: '#9ca3af' }} />
                      <Bar dataKey="income" fill="#22c55e" radius={[3, 3, 0, 0]} name={t('summary.income')} />
                      <Bar dataKey="expenses" fill="#ef4444" radius={[3, 3, 0, 0]} name={t('summary.expenses')} />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              )}
            </div>
          </div>

          {/* Net worth line chart */}
          <div className="bg-gray-800 rounded-xl p-6">
            <div className="flex items-center gap-2 mb-4">
              <TrendingUp size={16} className="text-blue-400" />
              <h2 className="font-semibold">{t('charts.netWorth')}</h2>
            </div>
            {netWorthData.length === 0 ? (
              <p className="text-gray-500 text-sm text-center py-8">{t('charts.noData')}</p>
            ) : (
              <div className="w-full h-56">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={netWorthData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                    <XAxis dataKey="month" tick={{ fill: '#9ca3af', fontSize: 11 }} />
                    <YAxis tick={{ fill: '#9ca3af', fontSize: 10 }} tickFormatter={fmt} />
                    <Tooltip
                      {...TOOLTIP_STYLE}
                      formatter={(value) => [fmt(Number(value)), t('charts.netWorth')] as [string, string]}
                    />
                    <Line
                      type="monotone"
                      dataKey="value"
                      stroke="#3b82f6"
                      strokeWidth={2}
                      dot={{ r: 3 }}
                      name={t('charts.netWorth')}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            )}
          </div>

          {/* Year-over-year */}
          {yoy && (
            <div className="bg-gray-800 rounded-xl p-6">
              <div className="flex items-center gap-2 mb-4">
                <BarChart2 size={16} className="text-purple-400" />
                <h2 className="font-semibold">
                  {t('charts.yearOverYear', {
                    current: yoy.current_year,
                    previous: yoy.previous_year,
                  })}
                </h2>
              </div>
              {yoyData.every(d => d.current === 0 && d.previous === 0) ? (
                <p className="text-gray-500 text-sm text-center py-8">{t('charts.noData')}</p>
              ) : (
                <div className="w-full h-56">
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart data={yoyData} margin={{ top: 5, right: 10, left: 0, bottom: 5 }}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                      <XAxis dataKey="month" tick={{ fill: '#9ca3af', fontSize: 11 }} />
                      <YAxis tick={{ fill: '#9ca3af', fontSize: 10 }} tickFormatter={fmt} />
                      <Tooltip
                        {...TOOLTIP_STYLE}
                        formatter={(value) => [fmt(Number(value)), ''] as [string, string]}
                      />
                      <Legend wrapperStyle={{ fontSize: 12, color: '#9ca3af' }} />
                      <Bar dataKey="current" fill="#3b82f6" radius={[3, 3, 0, 0]} name={String(yoy.current_year)} />
                      <Bar dataKey="previous" fill="#6b7280" radius={[3, 3, 0, 0]} name={String(yoy.previous_year)} />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              )}
            </div>
          )}

        </div>
      )}
    </div>
  )
}
