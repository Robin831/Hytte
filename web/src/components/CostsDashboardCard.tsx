import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { DollarSign, TrendingUp, BarChart2, AlertCircle } from 'lucide-react'
import { CollapsiblePanelHeader } from './CollapsiblePanelHeader'
import { usePanelCollapse } from '../hooks/usePanelCollapse'
import { formatDate } from '../utils/formatDate'
import {
  ResponsiveContainer,
  LineChart,
  BarChart,
  Line,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
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

interface BeadCost {
  bead_id: string
  estimated_cost: number
  input_tokens: number
  output_tokens: number
}

export default function CostsDashboardCard() {
  const { t } = useTranslation('forge')
  const [isOpen, toggle] = usePanelCollapse('costs')
  const [todayCosts, setTodayCosts] = useState<CostSummary | null>(null)
  const [weekCosts, setWeekCosts] = useState<CostSummary | null>(null)
  const [trend, setTrend] = useState<DailyCostEntry[]>([])
  const [topBeads, setTopBeads] = useState<BeadCost[]>([])
  const [loading, setLoading] = useState(true)
  const [anySucceeded, setAnySucceeded] = useState(false)

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const [todayRes, weekRes, trendRes, beadsRes] = await Promise.all([
          fetch('/api/forge/costs?period=today', { credentials: 'include' }),
          fetch('/api/forge/costs?period=week', { credentials: 'include' }),
          fetch('/api/forge/costs/trend?days=7', { credentials: 'include' }),
          fetch('/api/forge/costs/beads?days=7&limit=5', { credentials: 'include' }),
        ])
        if (cancelled) return
        let succeeded = false
        if (todayRes.ok) { setTodayCosts((await todayRes.json()) as CostSummary); succeeded = true }
        if (weekRes.ok) { setWeekCosts((await weekRes.json()) as CostSummary); succeeded = true }
        if (trendRes.ok) { setTrend((await trendRes.json()) as DailyCostEntry[]); succeeded = true }
        if (beadsRes.ok) { setTopBeads((await beadsRes.json()) as BeadCost[]); succeeded = true }
        setAnySucceeded(succeeded)
      } catch {
        // non-fatal — will show unavailable state below
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [])

  const formatCost = (v: number) =>
    new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: 4,
    }).format(v)

  const formatDateLabel = (date: string) => {
    try {
      // Parse YYYY-MM-DD as local date to avoid UTC-offset day shift.
      const [year, month, day] = date.split('-').map(Number)
      return formatDate(new Date(year, month - 1, day), { month: 'short', day: 'numeric' })
    } catch {
      return date.slice(5) // fallback: MM-DD
    }
  }

  if (loading) return null

  if (!anySucceeded) {
    return (
      <div id="costs" className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
        <CollapsiblePanelHeader
          isOpen={isOpen}
          toggle={toggle}
          panelId="costs-panel"
          icon={<DollarSign size={18} className="text-green-400 shrink-0" />}
          title={t('costs.title')}
        />
        {isOpen && (
        <div id="costs-panel">
          <div className="p-5 flex items-center gap-2 text-sm text-gray-400">
            <AlertCircle size={16} className="text-amber-400 shrink-0" />
            {t('costs.unavailable')}
          </div>
        </div>
        )}
      </div>
    )
  }

  const todayCost = todayCosts?.estimated_cost ?? 0
  const weekCost = weekCosts?.estimated_cost ?? 0
  const dailyLimit = todayCosts?.cost_limit ?? 0
  const budgetPct = dailyLimit > 0 ? Math.min(100, (todayCost / dailyLimit) * 100) : 0

  return (
    <div id="costs" className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <CollapsiblePanelHeader
        isOpen={isOpen}
        toggle={toggle}
        panelId="costs-panel"
        icon={<DollarSign size={18} className="text-green-400 shrink-0" />}
        title={t('costs.title')}
      />

      {isOpen && (
      <div id="costs-panel">
      <div className="p-5 flex flex-col gap-6">
        {/* Summary */}
        <div className="grid grid-cols-2 gap-4">
          <div>
            <span className="text-xs text-gray-500">{t('costs.today')}</span>
            <p className="text-lg font-semibold text-white mt-0.5">{formatCost(todayCost)}</p>
            {dailyLimit > 0 && (
              <p className="text-xs text-gray-400 mt-0.5">
                {t('costs.ofLimit', { limit: formatCost(dailyLimit) })}
              </p>
            )}
          </div>
          <div>
            <span className="text-xs text-gray-500">{t('costs.thisWeek')}</span>
            <p className="text-lg font-semibold text-white mt-0.5">{formatCost(weekCost)}</p>
          </div>
        </div>

        {/* Daily budget indicator */}
        {dailyLimit > 0 && (
          <div>
            <div className="flex items-center justify-between text-xs text-gray-400 mb-1.5">
              <span>{t('costs.dailyBudget')}</span>
              <span>{Math.round(budgetPct)}%</span>
            </div>
            <div className="h-2 bg-gray-700 rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full transition-all ${
                  budgetPct > 90
                    ? 'bg-red-500'
                    : budgetPct > 70
                      ? 'bg-amber-500'
                      : 'bg-green-500'
                }`}
                style={{ width: `${budgetPct}%` }}
              />
            </div>
          </div>
        )}

        {/* Weekly trend line chart */}
        {trend.length > 0 && (
          <div>
            <p className="text-xs text-gray-400 mb-3 flex items-center gap-1.5">
              <TrendingUp size={12} />
              {t('costs.weeklyTrend')}
            </p>
            <div className="h-36">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart
                  data={trend}
                  margin={{ top: 4, right: 8, left: 0, bottom: 4 }}
                  role="img"
                  aria-label={t('costs.weeklyTrend')}
                >
                  <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                  <XAxis
                    dataKey="date"
                    tickFormatter={formatDateLabel}
                    tick={{ fill: '#6b7280', fontSize: 10 }}
                  />
                  <YAxis
                    tick={{ fill: '#6b7280', fontSize: 10 }}
                    tickFormatter={(v: number) => formatCost(v)}
                    width={52}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: '#1f2937',
                      border: '1px solid #374151',
                      borderRadius: '8px',
                      color: '#e5e7eb',
                    }}
                    formatter={(value) => [typeof value === 'number' ? formatCost(value) : String(value ?? ''), t('costs.cost')]}
                    labelFormatter={(label: unknown) => typeof label === 'string' ? formatDateLabel(label) : String(label ?? '')}
                  />
                  <Line
                    type="monotone"
                    dataKey="estimated_cost"
                    stroke="#34d399"
                    strokeWidth={2}
                    dot={false}
                  />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </div>
        )}

        {/* Top beads bar chart */}
        {topBeads.length > 0 && (
          <div>
            <p className="text-xs text-gray-400 mb-3 flex items-center gap-1.5">
              <BarChart2 size={12} />
              {t('costs.topBeads')}
            </p>
            <div className="h-36">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart
                  data={topBeads}
                  margin={{ top: 4, right: 8, left: 0, bottom: 4 }}
                  role="img"
                  aria-label={t('costs.topBeads')}
                >
                  <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                  <XAxis dataKey="bead_id" tick={{ fill: '#6b7280', fontSize: 9 }} />
                  <YAxis
                    tick={{ fill: '#6b7280', fontSize: 10 }}
                    tickFormatter={(v: number) => formatCost(v)}
                    width={52}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: '#1f2937',
                      border: '1px solid #374151',
                      borderRadius: '8px',
                      color: '#e5e7eb',
                    }}
                    formatter={(value) => [typeof value === 'number' ? formatCost(value) : String(value ?? ''), t('costs.cost')]}
                  />
                  <Bar dataKey="estimated_cost" fill="#818cf8" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
        )}
      </div>
      </div>
      )}
    </div>
  )
}
