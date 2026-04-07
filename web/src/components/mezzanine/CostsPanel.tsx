import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AlertCircle, AlertTriangle, Bell, Settings2 } from 'lucide-react'
import { formatDate } from '../../utils/formatDate'
import {
  ResponsiveContainer,
  AreaChart,
  BarChart,
  Area,
  Bar,
  XAxis,
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

interface AnvilCost {
  anvil: string
  estimated_cost: number
  bead_count: number
}

interface BudgetConfig {
  alertThreshold: number // percentage (0-100) at which to show warning
  dailyBudget: number   // override for daily budget display (0 = use server value)
}

const BUDGET_CONFIG_KEY = 'mezzanine-budget-config'

function loadBudgetConfig(): BudgetConfig {
  try {
    const raw = localStorage.getItem(BUDGET_CONFIG_KEY)
    if (raw) return JSON.parse(raw) as BudgetConfig
  } catch { /* use defaults */ }
  return { alertThreshold: 80, dailyBudget: 0 }
}

function saveBudgetConfig(config: BudgetConfig) {
  localStorage.setItem(BUDGET_CONFIG_KEY, JSON.stringify(config))
}

export default function CostsPanel() {
  const { t } = useTranslation('forge')
  const [todayCosts, setTodayCosts] = useState<CostSummary | null>(null)
  const [trend, setTrend] = useState<DailyCostEntry[]>([])
  const [anvilCosts, setAnvilCosts] = useState<AnvilCost[]>([])
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [budgetConfig, setBudgetConfig] = useState<BudgetConfig>(loadBudgetConfig)
  const [showBudgetConfig, setShowBudgetConfig] = useState(false)

  const updateBudgetConfig = useCallback((updates: Partial<BudgetConfig>) => {
    setBudgetConfig(prev => {
      const next = { ...prev, ...updates }
      saveBudgetConfig(next)
      return next
    })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      try {
        const opts: RequestInit = { credentials: 'include', signal: controller.signal }
        const [todayResult, trendResult, anvilResult] = await Promise.allSettled([
          fetch('/api/forge/costs?period=today', opts),
          fetch('/api/forge/costs/trend?days=7', opts),
          fetch('/api/forge/costs/anvils?days=7', opts),
        ])
        if (controller.signal.aborted) return
        let ok = false
        if (todayResult.status === 'fulfilled' && todayResult.value.ok) {
          setTodayCosts((await todayResult.value.json()) as CostSummary)
          ok = true
        } else {
          setTodayCosts(null)
        }
        if (trendResult.status === 'fulfilled' && trendResult.value.ok) {
          setTrend((await trendResult.value.json()) as DailyCostEntry[])
          ok = true
        } else {
          setTrend([])
        }
        if (anvilResult.status === 'fulfilled' && anvilResult.value.ok) {
          setAnvilCosts((await anvilResult.value.json()) as AnvilCost[])
          ok = true
        } else {
          setAnvilCosts([])
        }
        setFailed(!ok)
      } catch {
        if (controller.signal.aborted) return
        setTodayCosts(null)
        setTrend([])
        setAnvilCosts([])
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

  const formatDateLabel = (date: string) => {
    try {
      const [year, month, day] = date.split('-').map(Number)
      return formatDate(new Date(year, month - 1, day), { month: 'short', day: 'numeric' })
    } catch {
      return date.slice(5)
    }
  }

  if (loading) return null

  if (failed && !todayCosts && trend.length === 0 && anvilCosts.length === 0) {
    return (
      <div className="flex flex-col rounded-lg border border-gray-700/50 bg-gray-900/60 overflow-hidden">
        <div className="flex items-center justify-between px-3 py-2 border-b border-gray-700/50">
          <h3 className="text-sm font-semibold text-gray-200">{t('mezzanine.costs.title')}</h3>
        </div>
        <div className="px-3 py-4 flex items-center gap-2 text-sm text-gray-500">
          <AlertCircle size={16} className="text-amber-400 shrink-0" />
          {t('mezzanine.costs.unavailable')}
        </div>
      </div>
    )
  }

  const todayCost = todayCosts?.estimated_cost ?? 0
  const serverLimit = todayCosts?.cost_limit ?? 0
  const dailyLimit = budgetConfig.dailyBudget > 0 ? budgetConfig.dailyBudget : serverLimit
  const budgetPct = dailyLimit > 0 ? Math.min(100, (todayCost / dailyLimit) * 100) : 0
  const isOverThreshold = dailyLimit > 0 && budgetPct >= budgetConfig.alertThreshold
  const maxAnvilCost = anvilCosts.length > 0 ? Math.max(...anvilCosts.map(a => a.estimated_cost)) : 0

  return (
    <div className="flex flex-col rounded-lg border border-gray-700/50 bg-gray-900/60 overflow-hidden">
      <div className="flex items-center justify-between px-3 py-2 border-b border-gray-700/50">
        <h3 className="text-sm font-semibold text-gray-200 flex items-center gap-1.5">
          {t('mezzanine.costs.title')}
          {isOverThreshold && (
            <AlertTriangle size={14} className="text-amber-400" aria-label={t('mezzanine.costs.budgetAlert')} />
          )}
        </h3>
        <button
          type="button"
          onClick={() => setShowBudgetConfig(prev => !prev)}
          aria-label={t('mezzanine.costs.configLabel')}
          className="text-gray-500 hover:text-gray-300 transition-colors"
        >
          <Settings2 size={14} />
        </button>
      </div>

      {/* Budget configuration panel */}
      {showBudgetConfig && (
        <div className="px-3 py-2 border-b border-gray-700/50 bg-gray-800/50 flex flex-col gap-2">
          <div className="flex items-center justify-between text-xs">
            <label htmlFor="budget-alert-threshold" className="text-gray-400">{t('mezzanine.costs.alertThreshold')}</label>
            <span className="text-gray-300 tabular-nums">{budgetConfig.alertThreshold}%</span>
          </div>
          <input
            id="budget-alert-threshold"
            type="range"
            min={50}
            max={100}
            step={5}
            value={budgetConfig.alertThreshold}
            onChange={e => updateBudgetConfig({ alertThreshold: Number(e.target.value) })}
            className="w-full h-1 bg-gray-700 rounded-lg appearance-none cursor-pointer accent-amber-500"
          />
          <div className="flex items-center justify-between text-xs">
            <label htmlFor="budget-daily-override" className="text-gray-400">{t('mezzanine.costs.dailyBudgetOverride')}</label>
            <span className="text-gray-300 tabular-nums">
              {budgetConfig.dailyBudget > 0 ? formatCost(budgetConfig.dailyBudget) : t('mezzanine.costs.serverDefault')}
            </span>
          </div>
          <input
            id="budget-daily-override"
            type="number"
            min={0}
            step={0.5}
            value={budgetConfig.dailyBudget || ''}
            placeholder="0"
            onChange={e => {
              const parsed = Number(e.target.value)
              updateBudgetConfig({ dailyBudget: !isNaN(parsed) && parsed >= 0 ? parsed : 0 })
            }}
            className="w-full px-2 py-1 text-xs rounded bg-gray-800 border border-gray-700 text-gray-300 focus:outline-none focus:ring-1 focus:ring-amber-500"
          />
        </div>
      )}

      <div className="flex-1 overflow-y-auto max-h-64 md:max-h-80">
        <div className="px-3 py-3 flex flex-col gap-3">
          {/* Budget alert banner */}
          {isOverThreshold && (
            <div className="flex items-center gap-2 px-2.5 py-1.5 rounded bg-amber-900/30 border border-amber-700/40 text-xs text-amber-300">
              <Bell size={12} className="shrink-0" />
              {t('mezzanine.costs.alertMessage', { percent: Math.round(budgetPct) })}
            </div>
          )}

          {/* Today's spend vs daily limit progress bar */}
          <div>
            <div className="flex items-center justify-between text-xs mb-1">
              <span className="text-gray-400">{t('mezzanine.costs.todaySpend')}</span>
              <span className="text-gray-300 font-medium tabular-nums">
                {formatCost(todayCost)}
                {dailyLimit > 0 && (
                  <span className="text-gray-500"> / {formatCost(dailyLimit)}</span>
                )}
              </span>
            </div>
            {dailyLimit > 0 && (
              <div
                className="h-2 bg-gray-700 rounded-full overflow-hidden"
                role="progressbar"
                aria-valuenow={Math.round(budgetPct)}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-label={t('mezzanine.costs.todaySpend')}
              >
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
            )}
          </div>

          {/* 7-day sparkline */}
          {trend.length > 1 && (
            <div>
              <p className="text-xs text-gray-500 mb-1">{t('mezzanine.costs.weeklySpend')}</p>
              <div className="h-16">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart
                    data={trend}
                    margin={{ top: 2, right: 2, left: 2, bottom: 2 }}
                    role="img"
                    aria-label={t('mezzanine.costs.weeklySpend')}
                  >
                    <defs>
                      <linearGradient id="costGradient" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#34d399" stopOpacity={0.3} />
                        <stop offset="95%" stopColor="#34d399" stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <Tooltip
                      contentStyle={{
                        backgroundColor: '#1f2937',
                        border: '1px solid #374151',
                        borderRadius: '6px',
                        color: '#e5e7eb',
                        fontSize: '11px',
                        padding: '4px 8px',
                      }}
                      formatter={(value) => [typeof value === 'number' ? formatCost(value) : String(value ?? ''), t('mezzanine.costs.cost')]}
                      labelFormatter={(label: unknown) => typeof label === 'string' ? formatDateLabel(label) : String(label ?? '')}
                    />
                    <Area
                      type="monotone"
                      dataKey="estimated_cost"
                      stroke="#34d399"
                      strokeWidth={1.5}
                      fill="url(#costGradient)"
                      dot={false}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            </div>
          )}

          {/* Per-anvil breakdown */}
          {anvilCosts.length > 0 && (
            <div>
              <p className="text-xs text-gray-500 mb-1.5">{t('mezzanine.costs.perAnvil')}</p>
              {anvilCosts.length <= 4 ? (
                <div className="h-20">
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart
                      data={anvilCosts}
                      margin={{ top: 2, right: 4, left: 4, bottom: 2 }}
                      role="img"
                      aria-label={t('mezzanine.costs.perAnvil')}
                    >
                      <XAxis
                        dataKey="anvil"
                        tick={{ fill: '#6b7280', fontSize: 10 }}
                        axisLine={false}
                        tickLine={false}
                      />
                      <Tooltip
                        contentStyle={{
                          backgroundColor: '#1f2937',
                          border: '1px solid #374151',
                          borderRadius: '6px',
                          color: '#e5e7eb',
                          fontSize: '11px',
                          padding: '4px 8px',
                        }}
                        formatter={(value) => [typeof value === 'number' ? formatCost(value) : String(value ?? ''), t('mezzanine.costs.cost')]}
                      />
                      <Bar
                        dataKey="estimated_cost"
                        fill="#818cf8"
                        radius={[3, 3, 0, 0]}
                      />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              ) : (
                <ul className="space-y-1.5">
                  {anvilCosts.map(a => (
                    <li key={a.anvil} className="flex items-center gap-2 text-xs">
                      <span className="text-gray-400 truncate min-w-0 flex-1">{a.anvil}</span>
                      <div className="w-20 h-1.5 bg-gray-700 rounded-full overflow-hidden shrink-0">
                        <div
                          className="h-full bg-indigo-400 rounded-full"
                          style={{ width: maxAnvilCost > 0 ? `${(a.estimated_cost / maxAnvilCost) * 100}%` : '0%' }}
                        />
                      </div>
                      <span className="text-gray-300 tabular-nums shrink-0">{formatCost(a.estimated_cost)}</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      </div>

      <div className="px-3 py-2 border-t border-gray-700/50">
        <Link
          to="/forge/mezzanine/costs"
          className="text-xs text-blue-400 hover:text-blue-300 hover:underline"
        >
          {t('mezzanine.costs.viewDashboard')}
        </Link>
      </div>
    </div>
  )
}
