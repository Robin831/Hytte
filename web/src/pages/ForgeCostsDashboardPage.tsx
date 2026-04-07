import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, AlertCircle, TrendingUp, BarChart2, Table } from 'lucide-react'
import { formatDate } from '../utils/formatDate'
import {
  ResponsiveContainer,
  AreaChart,
  BarChart,
  Area,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts'

interface CostSummary {
  period: string
  input_tokens: number
  output_tokens: number
  cache_read: number
  cache_write: number
  estimated_cost: number
  cost_limit: number
}

interface DailyCostEntry {
  date: string
  estimated_cost: number
  cost_limit: number
}

interface AnvilCost {
  anvil: string
  estimated_cost: number
  bead_count: number
}

interface BeadCost {
  bead_id: string
  estimated_cost: number
  input_tokens: number
  output_tokens: number
  cache_read: number
  cache_write: number
}

interface AnvilDailyCost {
  date: string
  [anvil: string]: number | string
}

const ANVIL_COLORS = [
  '#818cf8', // indigo
  '#34d399', // emerald
  '#f97316', // orange
  '#f472b6', // pink
  '#38bdf8', // sky
  '#a78bfa', // violet
  '#fbbf24', // amber
  '#4ade80', // green
]

export default function ForgeCostsDashboardPage() {
  const { t } = useTranslation('forge')
  const [todayCosts, setTodayCosts] = useState<CostSummary | null>(null)
  const [monthCosts, setMonthCosts] = useState<CostSummary | null>(null)
  const [trend, setTrend] = useState<DailyCostEntry[]>([])
  const [anvilCosts, setAnvilCosts] = useState<AnvilCost[]>([])
  const [topBeads, setTopBeads] = useState<BeadCost[]>([])
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      try {
        const opts: RequestInit = { credentials: 'include', signal: controller.signal }
        const [todayRes, monthRes, trendRes, anvilRes, beadsRes] = await Promise.allSettled([
          fetch('/api/forge/costs?period=today', opts),
          fetch('/api/forge/costs?period=month', opts),
          fetch('/api/forge/costs/trend?days=30', opts),
          fetch('/api/forge/costs/anvils?days=30', opts),
          fetch('/api/forge/costs/beads?days=30&limit=10', opts),
        ])
        if (controller.signal.aborted) return
        let ok = false
        if (todayRes.status === 'fulfilled' && todayRes.value.ok) {
          setTodayCosts((await todayRes.value.json()) as CostSummary)
          ok = true
        }
        if (monthRes.status === 'fulfilled' && monthRes.value.ok) {
          setMonthCosts((await monthRes.value.json()) as CostSummary)
          ok = true
        }
        if (trendRes.status === 'fulfilled' && trendRes.value.ok) {
          setTrend((await trendRes.value.json()) as DailyCostEntry[])
          ok = true
        }
        if (anvilRes.status === 'fulfilled' && anvilRes.value.ok) {
          setAnvilCosts((await anvilRes.value.json()) as AnvilCost[])
          ok = true
        }
        if (beadsRes.status === 'fulfilled' && beadsRes.value.ok) {
          setTopBeads((await beadsRes.value.json()) as BeadCost[])
          ok = true
        }
        setFailed(!ok)
      } catch {
        if (controller.signal.aborted) return
        setFailed(true)
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    void load()
    return () => { controller.abort() }
  }, [])

  const formatCost = (v: number) =>
    new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: 4,
    }).format(v)

  const formatCostShort = (v: number) =>
    new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(v)

  const formatDateLabel = (date: string) => {
    try {
      const [year, month, day] = date.split('-').map(Number)
      return formatDate(new Date(year, month - 1, day), { month: 'short', day: 'numeric' })
    } catch {
      return date.slice(5)
    }
  }

  const formatTokens = (n: number) => {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`
    return String(n)
  }

  // Build stacked bar chart data: one entry per date, each anvil as a separate key
  const anvilNames = [...new Set(anvilCosts.map(a => a.anvil))].sort()
  const stackedData: AnvilDailyCost[] = (() => {
    // Use the trend dates as the x-axis, distribute anvil costs proportionally
    // Since we only have aggregate anvil costs, build a simple bar chart instead
    return anvilCosts.map(a => ({
      date: a.anvil,
      cost: a.estimated_cost,
      beads: a.bead_count,
    })) as unknown as AnvilDailyCost[]
  })()

  const tooltipStyle = {
    backgroundColor: '#1f2937',
    border: '1px solid #374151',
    borderRadius: '8px',
    color: '#e5e7eb',
  }

  if (loading) {
    return (
      <div className="p-4 sm:p-6 max-w-6xl mx-auto">
        <div className="animate-pulse space-y-6">
          <div className="h-8 bg-gray-800 rounded w-48" />
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            {[1, 2, 3].map(i => <div key={i} className="h-24 bg-gray-800 rounded-lg" />)}
          </div>
          <div className="h-64 bg-gray-800 rounded-lg" />
        </div>
      </div>
    )
  }

  if (failed) {
    return (
      <div className="p-4 sm:p-6 max-w-6xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Link to="/forge/mezzanine" className="text-gray-400 hover:text-white">
            <ArrowLeft size={20} />
          </Link>
          <h1 className="text-xl font-bold text-white">{t('costsDashboard.title')}</h1>
        </div>
        <div className="flex items-center gap-2 text-gray-400">
          <AlertCircle size={16} className="text-amber-400 shrink-0" />
          {t('costs.unavailable')}
        </div>
      </div>
    )
  }

  const todayCost = todayCosts?.estimated_cost ?? 0
  const monthCost = monthCosts?.estimated_cost ?? 0
  const dailyLimit = todayCosts?.cost_limit ?? 0
  const budgetPct = dailyLimit > 0 ? Math.min(100, (todayCost / dailyLimit) * 100) : 0

  return (
    <div className="p-4 sm:p-6 max-w-6xl mx-auto">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Link to="/forge/mezzanine" className="text-gray-400 hover:text-white">
          <ArrowLeft size={20} />
        </Link>
        <h1 className="text-xl font-bold text-white">{t('costsDashboard.title')}</h1>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-6">
        <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-4">
          <span className="text-xs text-gray-500">{t('costsDashboard.todayCost')}</span>
          <p className="text-xl font-semibold text-white mt-1">{formatCost(todayCost)}</p>
          {dailyLimit > 0 && (
            <>
              <p className="text-xs text-gray-400 mt-1">
                {t('costs.ofLimit', { limit: formatCost(dailyLimit) })}
              </p>
              <div className="h-1.5 bg-gray-700 rounded-full overflow-hidden mt-2">
                <div
                  className={`h-full rounded-full transition-all ${
                    budgetPct > 90 ? 'bg-red-500' : budgetPct > 70 ? 'bg-amber-500' : 'bg-green-500'
                  }`}
                  style={{ width: `${budgetPct}%` }}
                />
              </div>
            </>
          )}
        </div>
        <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-4">
          <span className="text-xs text-gray-500">{t('costsDashboard.monthCost')}</span>
          <p className="text-xl font-semibold text-white mt-1">{formatCost(monthCost)}</p>
        </div>
        <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-4">
          <span className="text-xs text-gray-500">{t('costsDashboard.totalBeads')}</span>
          <p className="text-xl font-semibold text-white mt-1">
            {anvilCosts.reduce((sum, a) => sum + a.bead_count, 0)}
          </p>
          <p className="text-xs text-gray-400 mt-1">
            {t('costsDashboard.across', { count: anvilNames.length })}
          </p>
        </div>
      </div>

      {/* Daily costs area chart — 30 days */}
      {trend.length > 0 && (
        <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-4 mb-6">
          <h2 className="text-sm font-semibold text-gray-200 mb-3 flex items-center gap-1.5">
            <TrendingUp size={14} />
            {t('costsDashboard.dailyTrend')}
          </h2>
          <div className="h-48 sm:h-64">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart
                data={trend}
                margin={{ top: 4, right: 8, left: 0, bottom: 4 }}
                role="img"
                aria-label={t('costsDashboard.dailyTrend')}
              >
                <defs>
                  <linearGradient id="costDashGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#34d399" stopOpacity={0.3} />
                    <stop offset="95%" stopColor="#34d399" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis
                  dataKey="date"
                  tickFormatter={formatDateLabel}
                  tick={{ fill: '#6b7280', fontSize: 10 }}
                  interval="preserveStartEnd"
                />
                <YAxis
                  tick={{ fill: '#6b7280', fontSize: 10 }}
                  tickFormatter={(v: number) => formatCostShort(v)}
                  width={56}
                />
                <Tooltip
                  contentStyle={tooltipStyle}
                  formatter={(value) => [typeof value === 'number' ? formatCost(value) : String(value ?? ''), t('costsDashboard.dailyCost')]}
                  labelFormatter={(label: unknown) => typeof label === 'string' ? formatDateLabel(label) : String(label ?? '')}
                />
                {trend.some(d => d.cost_limit > 0) && (
                  <Area
                    type="monotone"
                    dataKey="cost_limit"
                    stroke="#ef4444"
                    strokeWidth={1}
                    strokeDasharray="4 4"
                    fill="none"
                    dot={false}
                    name={t('costsDashboard.dailyLimit')}
                  />
                )}
                <Area
                  type="monotone"
                  dataKey="estimated_cost"
                  stroke="#34d399"
                  strokeWidth={2}
                  fill="url(#costDashGradient)"
                  dot={false}
                  name={t('costsDashboard.dailyCost')}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Per-anvil stacked bar chart */}
      {anvilCosts.length > 0 && (
        <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-4 mb-6">
          <h2 className="text-sm font-semibold text-gray-200 mb-3 flex items-center gap-1.5">
            <BarChart2 size={14} />
            {t('costsDashboard.perAnvil')}
          </h2>
          <div className="h-48 sm:h-64">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart
                data={stackedData}
                margin={{ top: 4, right: 8, left: 0, bottom: 4 }}
                role="img"
                aria-label={t('costsDashboard.perAnvil')}
              >
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis
                  dataKey="date"
                  tick={{ fill: '#6b7280', fontSize: 10 }}
                />
                <YAxis
                  tick={{ fill: '#6b7280', fontSize: 10 }}
                  tickFormatter={(v: number) => formatCostShort(v)}
                  width={56}
                />
                <Tooltip
                  contentStyle={tooltipStyle}
                  formatter={(value, name) => [
                    typeof value === 'number' ? formatCost(value) : String(value ?? ''),
                    name === 'cost' ? t('mezzanine.costs.cost') : String(name),
                  ]}
                />
                <Legend />
                <Bar
                  dataKey="cost"
                  name={t('mezzanine.costs.cost')}
                  fill="#818cf8"
                  radius={[4, 4, 0, 0]}
                />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Top 10 most expensive beads table */}
      {topBeads.length > 0 && (
        <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-4">
          <h2 className="text-sm font-semibold text-gray-200 mb-3 flex items-center gap-1.5">
            <Table size={14} />
            {t('costsDashboard.topBeads')}
          </h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-700/50">
                  <th className="text-left text-xs text-gray-500 font-medium py-2 pr-4">{t('costsDashboard.colBead')}</th>
                  <th className="text-right text-xs text-gray-500 font-medium py-2 px-2">{t('costsDashboard.colCost')}</th>
                  <th className="text-right text-xs text-gray-500 font-medium py-2 px-2 hidden sm:table-cell">{t('costsDashboard.colInput')}</th>
                  <th className="text-right text-xs text-gray-500 font-medium py-2 px-2 hidden sm:table-cell">{t('costsDashboard.colOutput')}</th>
                  <th className="text-right text-xs text-gray-500 font-medium py-2 pl-2 hidden md:table-cell">{t('costsDashboard.colCacheRead')}</th>
                </tr>
              </thead>
              <tbody>
                {topBeads.map((bead, i) => (
                  <tr key={bead.bead_id} className={`border-b border-gray-700/30 ${i % 2 === 0 ? '' : 'bg-gray-800/50'}`}>
                    <td className="py-2 pr-4">
                      <span className="text-cyan-400 font-mono text-xs">{bead.bead_id}</span>
                    </td>
                    <td className="text-right py-2 px-2 tabular-nums text-white font-medium">
                      {formatCost(bead.estimated_cost)}
                    </td>
                    <td className="text-right py-2 px-2 tabular-nums text-gray-400 hidden sm:table-cell">
                      {formatTokens(bead.input_tokens)}
                    </td>
                    <td className="text-right py-2 px-2 tabular-nums text-gray-400 hidden sm:table-cell">
                      {formatTokens(bead.output_tokens)}
                    </td>
                    <td className="text-right py-2 pl-2 tabular-nums text-gray-400 hidden md:table-cell">
                      {formatTokens(bead.cache_read)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}
