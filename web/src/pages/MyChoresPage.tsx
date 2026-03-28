import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, Clock, XCircle, Coins, Target, Plus } from 'lucide-react'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import { formatDate } from '../utils/formatDate'

interface ChoreWithStatus {
  id: number
  name: string
  description: string
  amount: number
  currency: string
  frequency: string
  icon: string
  requires_approval: boolean
  completion_id?: number
  completion_status?: string
}

interface WeeklyEarnings {
  child_id: number
  week_start: string
  base_allowance: number
  chore_earnings: number
  bonus_amount: number
  total_amount: number
  currency: string
  approved_count: number
}

interface Payout {
  id: number
  week_start: string
  base_amount: number
  bonus_amount: number
  total_amount: number
  currency: string
  paid_out: boolean
  paid_at?: string
}

interface Extra {
  id: number
  name: string
  amount: number
  currency: string
  status: string
  expires_at: string | null
}

interface SavingsGoal {
  id: number
  name: string
  target_amount: number
  current_amount: number
  currency: string
  deadline?: string
  weeks_remaining?: number
}

type Tab = 'chores' | 'earnings' | 'extras' | 'goals'

export default function MyChoresPage() {
  const { t, i18n } = useTranslation('allowance')
  const [tab, setTab] = useState<Tab>('chores')

  const [chores, setChores] = useState<ChoreWithStatus[]>([])
  const [choresLoading, setChoresLoading] = useState(true)
  const [choresError, setChoresError] = useState('')

  const [earnings, setEarnings] = useState<WeeklyEarnings | null>(null)
  const [history, setHistory] = useState<Payout[]>([])
  const [earningsLoading, setEarningsLoading] = useState(false)
  const [earningsError, setEarningsError] = useState('')

  const [extras, setExtras] = useState<Extra[]>([])
  const [extrasLoading, setExtrasLoading] = useState(false)
  const [extrasError, setExtrasError] = useState('')

  const [completing, setCompleting] = useState<number | null>(null)
  const [actionError, setActionError] = useState('')

  const [claiming, setClaiming] = useState<number | null>(null)
  const [claimError, setClaimError] = useState('')

  const [goals, setGoals] = useState<SavingsGoal[]>([])
  const [goalsLoading, setGoalsLoading] = useState(false)
  const [goalsError, setGoalsError] = useState('')
  const [showGoalForm, setShowGoalForm] = useState(false)
  const [goalForm, setGoalForm] = useState({ name: '', target_amount: '', deadline: '' })
  const [goalFormSaving, setGoalFormSaving] = useState(false)
  const [goalFormError, setGoalFormError] = useState('')
  const [updatingSaved, setUpdatingSaved] = useState<number | null>(null)
  const [savedInput, setSavedInput] = useState<Record<number, string>>({})

  const loadChores = useCallback(async (signal?: AbortSignal) => {
    setChoresLoading(true)
    setChoresError('')
    try {
      const res = await fetch('/api/allowance/my/chores', { credentials: 'include', signal })
      if (!res.ok) throw new Error()
      const json: { chores?: ChoreWithStatus[] } = await res.json()
      setChores(json?.chores ?? [])
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setChoresError(t('errors.loadFailed'))
    } finally {
      setChoresLoading(false)
    }
  }, [t])

  const loadEarnings = useCallback(async (signal?: AbortSignal) => {
    setEarningsLoading(true)
    setEarningsError('')
    try {
      const [earRes, histRes] = await Promise.all([
        fetch('/api/allowance/my/earnings', { credentials: 'include', signal }),
        fetch('/api/allowance/my/history', { credentials: 'include', signal }),
      ])
      if (!earRes.ok || !histRes.ok) throw new Error()
      const earData: WeeklyEarnings = await earRes.json()
      const histJson: { payouts?: Payout[] } = await histRes.json()
      const payouts = histJson?.payouts ?? []
      setEarnings(earData)
      setHistory(payouts)
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setEarningsError(t('errors.loadFailed'))
    } finally {
      setEarningsLoading(false)
    }
  }, [t])

  const loadExtras = useCallback(async (signal?: AbortSignal) => {
    setExtrasLoading(true)
    setExtrasError('')
    try {
      const res = await fetch('/api/allowance/my/extras', { credentials: 'include', signal })
      if (!res.ok) throw new Error()
      const json: { extras?: Extra[] } = await res.json()
      setExtras(json?.extras ?? [])
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setExtrasError(t('errors.loadFailed'))
    } finally {
      setExtrasLoading(false)
    }
  }, [t])

  const loadGoals = useCallback(async (signal?: AbortSignal) => {
    setGoalsLoading(true)
    setGoalsError('')
    try {
      const res = await fetch('/api/allowance/my/goals', { credentials: 'include', signal })
      if (!res.ok) throw new Error()
      const json: { goals?: SavingsGoal[] } = await res.json()
      setGoals(json?.goals ?? [])
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setGoalsError(t('errors.loadFailed'))
    } finally {
      setGoalsLoading(false)
    }
  }, [t])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    loadChores(controller.signal)
    return () => controller.abort()
  }, [loadChores])

  useEffect(() => {
    if (tab === 'earnings') {
      const controller = new AbortController()
      // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
      loadEarnings(controller.signal)
      return () => controller.abort()
    }
  }, [tab, loadEarnings])

  useEffect(() => {
    if (tab === 'extras') {
      const controller = new AbortController()
      // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
      loadExtras(controller.signal)
      return () => controller.abort()
    }
  }, [tab, loadExtras])

  useEffect(() => {
    if (tab === 'goals') {
      const controller = new AbortController()
      // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
      loadGoals(controller.signal)
      return () => controller.abort()
    }
  }, [tab, loadGoals])

  const handleComplete = async (choreId: number) => {
    setCompleting(choreId)
    setActionError('')
    try {
      const res = await fetch(`/api/allowance/my/complete/${choreId}`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error()
      // Refresh chores to get updated status
      await loadChores()
    } catch {
      setActionError(t('errors.actionFailed'))
    } finally {
      setCompleting(null)
    }
  }

  const handleClaimExtra = async (extraId: number) => {
    setClaiming(extraId)
    setClaimError('')
    try {
      const res = await fetch(`/api/allowance/my/claim-extra/${extraId}`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error()
      // Remove the claimed extra from the list (it's no longer open)
      setExtras(prev => prev.filter(e => e.id !== extraId))
    } catch {
      setClaimError(t('errors.actionFailed'))
    } finally {
      setClaiming(null)
    }
  }

  const handleCreateGoal = async () => {
    if (!goalForm.name.trim()) {
      setGoalFormError(t('errors.nameRequired'))
      return
    }
    const target = parseFloat(goalForm.target_amount)
    if (isNaN(target) || target <= 0) {
      setGoalFormError(t('errors.amountInvalid'))
      return
    }
    setGoalFormSaving(true)
    setGoalFormError('')
    try {
      const body: { name: string; target_amount: number; deadline?: string } = {
        name: goalForm.name.trim(),
        target_amount: target,
      }
      if (goalForm.deadline) body.deadline = goalForm.deadline
      const res = await fetch('/api/allowance/my/goals', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) throw new Error()
      const created: SavingsGoal = await res.json()
      setGoals(prev => [created, ...prev])
      setShowGoalForm(false)
      setGoalForm({ name: '', target_amount: '', deadline: '' })
    } catch {
      setGoalFormError(t('errors.actionFailed'))
    } finally {
      setGoalFormSaving(false)
    }
  }

  const handleUpdateSaved = async (goalId: number) => {
    const val = parseFloat(savedInput[goalId] ?? '')
    if (isNaN(val) || val < 0) {
      setGoalsError(t('errors.amountInvalid'))
      return
    }
    setGoalsError('')
    setUpdatingSaved(goalId)
    try {
      const res = await fetch(`/api/allowance/my/goals/${goalId}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ current_amount: val }),
      })
      if (!res.ok) throw new Error()
      const updated: SavingsGoal = await res.json()
      setGoals(prev => prev.map(g => (g.id === updated.id ? updated : g)))
      setSavedInput(prev => ({ ...prev, [goalId]: '' }))
    } catch {
      setGoalsError(t('errors.actionFailed'))
    } finally {
      setUpdatingSaved(null)
    }
  }

  const formatAmount = (amount: number, currency: string) => {
    const curr = currency || t('currency')
    return `${amount} ${curr}`
  }

  const formatWeekRange = (weekStart: string) => {
    // weekStart is "YYYY-MM-DD"; parse as local midnight to avoid UTC off-by-one issues
    const start = new Date(`${weekStart}T00:00:00`)
    const end = new Date(start)
    end.setDate(end.getDate() + 6)
    const fmt = new Intl.DateTimeFormat(i18n.language, { month: 'short', day: 'numeric' })
    return `${fmt.format(start)} – ${fmt.format(end)}`
  }

  const doneChores = chores.filter(c => c.completion_status === 'approved')
  const pendingChores = chores.filter(c => c.completion_status === 'pending')
  const todoChores = chores.filter(
    c => !c.completion_status || c.completion_status === 'rejected',
  )

  return (
    <div className="max-w-lg mx-auto px-4 py-6 space-y-6">
      {/* Header */}
      <div className="text-center">
        <div className="text-5xl mb-2">🏠</div>
        <h1 className="text-2xl font-bold text-white">{t('myChores.title')}</h1>
      </div>

      {/* Tab bar */}
      <div role="tablist" className="flex gap-1 bg-gray-800 rounded-xl p-1 overflow-x-auto">
        {(['chores', 'extras', 'earnings', 'goals'] as const).map(id => (
          <button
            key={id}
            role="tab"
            aria-selected={tab === id}
            id={`tab-${id}`}
            onClick={() => setTab(id)}
            className={`flex-1 py-2 px-2 rounded-lg text-sm font-semibold transition-colors cursor-pointer whitespace-nowrap ${
              tab === id
                ? 'bg-yellow-400 text-gray-900'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            {id === 'chores' && t('myChores.tabs.chores')}
            {id === 'extras' && t('myChores.tabs.extras')}
            {id === 'earnings' && t('myChores.tabs.earnings')}
            {id === 'goals' && t('goals.title')}
          </button>
        ))}
      </div>

      {/* Chores tab */}
      {tab === 'chores' && (
        <div role="tabpanel" aria-labelledby="tab-chores" className="space-y-4">
          {choresLoading && (
            <p className="text-center text-gray-400 py-8">{t('loading')}</p>
          )}
          {choresError && (
            <p className="text-center text-red-400 py-4">{choresError}</p>
          )}
          {actionError && (
            <p className="text-center text-red-400 text-sm">{actionError}</p>
          )}

          {!choresLoading && !choresError && chores.length === 0 && (
            <div className="text-center py-12">
              <div className="text-4xl mb-3">🎉</div>
              <p className="text-gray-400">{t('myChores.noChores')}</p>
            </div>
          )}

          {/* To-do chores */}
          {todoChores.length > 0 && (
            <div className="space-y-3">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wider px-1">
                {t('myChores.todo')}
              </h2>
              {todoChores.map(chore => (
                <button
                  key={chore.id}
                  onClick={() => handleComplete(chore.id)}
                  disabled={completing === chore.id}
                  className="w-full bg-gray-800 hover:bg-gray-700 active:scale-95 rounded-2xl p-4 flex items-center gap-4 transition-all cursor-pointer disabled:opacity-60"
                >
                  <span className="text-4xl select-none">{chore.icon || '📋'}</span>
                  <div className="flex-1 text-left">
                    <p className="text-white font-semibold text-lg leading-tight">{chore.name}</p>
                    {chore.completion_status === 'rejected' && (
                      <div className="flex items-center gap-1.5 mt-1">
                        <XCircle size={14} className="text-red-400" />
                        <span className="text-red-400 text-sm font-medium">
                          {t('myChores.rejected')}
                        </span>
                      </div>
                    )}
                    {chore.description && (
                      <p className="text-gray-400 text-sm mt-0.5">{chore.description}</p>
                    )}
                  </div>
                  <div className="text-right shrink-0">
                    <span className="text-yellow-400 font-bold text-xl">
                      {formatAmount(chore.amount, chore.currency)}
                    </span>
                    {completing === chore.id ? (
                      <p className="text-gray-400 text-xs mt-1">{t('loading')}</p>
                    ) : (
                      <p className="text-gray-500 text-xs mt-1">{t('myChores.tap')}</p>
                    )}
                  </div>
                </button>
              ))}
            </div>
          )}

          {/* Pending approval chores */}
          {pendingChores.length > 0 && (
            <div className="space-y-3">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wider px-1">
                {t('myChores.waiting')}
              </h2>
              {pendingChores.map(chore => (
                <div
                  key={chore.id}
                  className="bg-gray-800 rounded-2xl p-4 flex items-center gap-4 opacity-80"
                >
                  <span className="text-4xl select-none">{chore.icon || '📋'}</span>
                  <div className="flex-1">
                    <p className="text-white font-semibold text-lg leading-tight">{chore.name}</p>
                    <div className="flex items-center gap-1.5 mt-1">
                      <Clock size={14} className="text-orange-400" />
                      <span className="text-orange-400 text-sm font-medium">
                        {t('myChores.waitingApproval')}
                      </span>
                    </div>
                  </div>
                  <span className="text-yellow-400 font-bold text-xl shrink-0">
                    {formatAmount(chore.amount, chore.currency)}
                  </span>
                </div>
              ))}
            </div>
          )}

          {/* Approved chores */}
          {doneChores.length > 0 && (
            <div className="space-y-3">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wider px-1">
                {t('myChores.done')}
              </h2>
              {doneChores.map(chore => (
                <div
                  key={chore.id}
                  className="bg-green-900/30 border border-green-700/40 rounded-2xl p-4 flex items-center gap-4"
                >
                  <span className="text-4xl select-none">{chore.icon || '📋'}</span>
                  <div className="flex-1">
                    <p className="text-white font-semibold text-lg leading-tight">{chore.name}</p>
                    <div className="flex items-center gap-1.5 mt-1">
                      <CheckCircle2 size={14} className="text-green-400" />
                      <span className="text-green-400 text-sm font-medium">
                        {t('myChores.approved')}
                      </span>
                    </div>
                  </div>
                  <span className="text-green-400 font-bold text-xl shrink-0">
                    +{formatAmount(chore.amount, chore.currency)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Extras tab — Extras Board */}
      {tab === 'extras' && (
        <div role="tabpanel" aria-labelledby="tab-extras" className="space-y-4">
          {extrasLoading && (
            <p className="text-center text-gray-400 py-8">{t('loading')}</p>
          )}
          {extrasError && (
            <p className="text-center text-red-400 py-4">{extrasError}</p>
          )}
          {claimError && (
            <p className="text-center text-red-400 text-sm">{claimError}</p>
          )}

          {!extrasLoading && !extrasError && extras.length === 0 && (
            <div className="text-center py-12">
              <div className="text-4xl mb-3">🎯</div>
              <p className="text-gray-400">{t('myChores.extras.noExtras')}</p>
            </div>
          )}

          {extras.length > 0 && (
            <div className="space-y-3">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wider px-1">
                {t('myChores.extras.board')}
              </h2>
              {extras.map(extra => {
                const displayCurrency = extra.currency === 'NOK' ? 'kr' : extra.currency

                return (
                  <div
                    key={extra.id}
                    className="bg-gray-800 rounded-2xl p-5 flex items-center gap-4"
                  >
                    <span className="text-4xl select-none shrink-0">🎯</span>
                    <div className="flex-1 min-w-0">
                      <p className="text-white font-semibold text-lg leading-tight">{extra.name}</p>
                      <p className="text-yellow-400 font-bold text-xl mt-1">
                        {formatAmount(extra.amount, displayCurrency)}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() => handleClaimExtra(extra.id)}
                      disabled={claiming === extra.id}
                      className="shrink-0 px-5 py-3 bg-yellow-400 hover:bg-yellow-300 active:scale-95 text-gray-900 rounded-xl font-bold text-sm transition-all cursor-pointer disabled:opacity-60 disabled:cursor-not-allowed"
                    >
                      {claiming === extra.id
                        ? t('myChores.extras.claiming')
                        : t('myChores.extras.claim')}
                    </button>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}

      {/* Earnings tab */}
      {tab === 'earnings' && (
        <div role="tabpanel" aria-labelledby="tab-earnings" className="space-y-4">
          {earningsLoading && (
            <p className="text-center text-gray-400 py-8">{t('loading')}</p>
          )}
          {earningsError && (
            <p className="text-center text-red-400 py-4">{earningsError}</p>
          )}

          {!earningsLoading && !earningsError && earnings && (
            <div className="bg-gradient-to-br from-yellow-500/20 to-orange-500/10 border border-yellow-500/30 rounded-2xl p-6 text-center">
              <Coins size={32} className="text-yellow-400 mx-auto mb-2" />
              <p className="text-gray-400 text-sm mb-1">{t('myChores.thisWeek')}</p>
              <p className="text-4xl font-bold text-yellow-400">
                {formatAmount(earnings.total_amount, earnings.currency)}
              </p>
              <div className="mt-4 grid grid-cols-3 gap-3 text-center">
                <div>
                  <p className="text-gray-400 text-xs">{t('breakdown.base')}</p>
                  <p className="text-white font-semibold">
                    {formatAmount(earnings.base_allowance, earnings.currency)}
                  </p>
                </div>
                <div>
                  <p className="text-gray-400 text-xs">{t('myChores.chores')}</p>
                  <p className="text-white font-semibold">
                    {formatAmount(earnings.chore_earnings, earnings.currency)}
                  </p>
                </div>
                <div>
                  <p className="text-gray-400 text-xs">{t('breakdown.bonus')}</p>
                  <p className="text-white font-semibold">
                    {formatAmount(earnings.bonus_amount, earnings.currency)}
                  </p>
                </div>
              </div>
              <p className="text-gray-500 text-xs mt-3">
                {t('myChores.approvedCount', { count: earnings.approved_count })}
              </p>
            </div>
          )}

          {/* Earnings history bar chart */}
          {!earningsLoading && !earningsError && history.length > 1 && (
            <div className="bg-gray-800 rounded-2xl p-4">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-3">
                {t('goals.historyChart')}
              </h2>
              <ResponsiveContainer width="100%" height={120}>
                <BarChart data={[...history].reverse()} margin={{ top: 0, right: 0, left: -20, bottom: 0 }}>
                  <XAxis
                    dataKey="week_start"
                    tickFormatter={(v: string) => {
                      const [year, month, day] = v.split('-').map(Number)
                      const d = new Date(Date.UTC(year, (month || 1) - 1, day || 1))
                      return formatDate(d, { month: 'short', day: 'numeric', timeZone: 'UTC' })
                    }}
                    tick={{ fill: '#9ca3af', fontSize: 10 }}
                    axisLine={false}
                    tickLine={false}
                  />
                  <YAxis tick={{ fill: '#9ca3af', fontSize: 10 }} axisLine={false} tickLine={false} />
                  <Tooltip
                    formatter={(value) => [`${value ?? ''} ${t('currency')}`, '']}
                    contentStyle={{ background: '#1f2937', border: 'none', borderRadius: 8, color: '#f9fafb' }}
                    cursor={{ fill: 'rgba(255,255,255,0.05)' }}
                  />
                  <Bar dataKey="total_amount" radius={[4, 4, 0, 0]}>
                    {[...history].reverse().map((p) => (
                      <Cell key={p.id} fill={p.paid_out ? '#4ade80' : '#facc15'} />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>
          )}

          {/* History */}
          {!earningsLoading && !earningsError && history.length > 0 && (
            <div className="space-y-3">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wider px-1">
                {t('myChores.history')}
              </h2>
              {history.map(payout => (
                <div
                  key={payout.id}
                  className="bg-gray-800 rounded-2xl p-4 flex items-center gap-4"
                >
                  <div className="flex-1">
                    <p className="text-white font-semibold">{formatWeekRange(payout.week_start)}</p>
                    <p className="text-gray-400 text-sm">
                      {t('breakdown.base')}: {formatAmount(payout.base_amount, payout.currency)}
                      {payout.bonus_amount > 0 && (
                        <> · {t('breakdown.bonus')}: {formatAmount(payout.bonus_amount, payout.currency)}</>
                      )}
                    </p>
                  </div>
                  <div className="text-right shrink-0">
                    <p className="text-yellow-400 font-bold text-lg">
                      {formatAmount(payout.total_amount, payout.currency)}
                    </p>
                    {payout.paid_out ? (
                      <span className="text-green-400 text-xs font-medium flex items-center gap-1 justify-end">
                        <CheckCircle2 size={12} />
                        {t('paid')}
                      </span>
                    ) : (
                      <span className="text-gray-500 text-xs">{t('myChores.pending')}</span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}

          {!earningsLoading && !earningsError && history.length === 0 && !earnings && (
            <div className="text-center py-12">
              <div className="text-4xl mb-3">💰</div>
              <p className="text-gray-400">{t('noPayouts')}</p>
            </div>
          )}

          {/* Rejected indicator */}
          {!earningsLoading && (
            <div className="flex items-center gap-2 text-gray-500 text-xs px-1">
              <XCircle size={12} />
              <span>{t('myChores.rejectedNote')}</span>
            </div>
          )}
        </div>
      )}

      {/* Goals tab */}
      {tab === 'goals' && (
        <div role="tabpanel" aria-labelledby="tab-goals" className="space-y-4">
          {goalsLoading && (
            <p className="text-center text-gray-400 py-8">{t('loading')}</p>
          )}
          {goalsError && (
            <p className="text-center text-red-400 py-4">{goalsError}</p>
          )}

          <div className="flex justify-end">
            <button
              type="button"
              onClick={() => setShowGoalForm(true)}
              className="flex items-center gap-2 px-4 py-2 bg-yellow-400 hover:bg-yellow-300 active:scale-95 text-gray-900 rounded-xl font-bold text-sm transition-all cursor-pointer"
            >
              <Plus size={16} />
              {t('goals.addGoal')}
            </button>
          </div>

          {showGoalForm && (
            <div className="bg-gray-800 rounded-2xl p-4 space-y-3">
              <h3 className="text-white font-semibold">{t('goals.newGoal')}</h3>
              <div>
                <label htmlFor="goal-name" className="block text-sm text-gray-400 mb-1">
                  {t('goals.goalName')}
                </label>
                <input
                  id="goal-name"
                  type="text"
                  value={goalForm.name}
                  onChange={e => setGoalForm(f => ({ ...f, name: e.target.value }))}
                  className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-yellow-400"
                  placeholder={t('goals.goalNamePlaceholder')}
                />
              </div>
              <div>
                <label htmlFor="goal-target" className="block text-sm text-gray-400 mb-1">
                  {t('goals.targetAmount')} ({t('currency')})
                </label>
                <input
                  id="goal-target"
                  type="number"
                  min="1"
                  step="1"
                  value={goalForm.target_amount}
                  onChange={e => setGoalForm(f => ({ ...f, target_amount: e.target.value }))}
                  className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-yellow-400"
                  placeholder="500"
                />
              </div>
              <div>
                <label htmlFor="goal-deadline" className="block text-sm text-gray-400 mb-1">
                  {t('goals.deadline')}
                </label>
                <input
                  id="goal-deadline"
                  type="date"
                  value={goalForm.deadline}
                  onChange={e => setGoalForm(f => ({ ...f, deadline: e.target.value }))}
                  className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-yellow-400"
                />
              </div>
              {goalFormError && (
                <p className="text-red-400 text-sm">{goalFormError}</p>
              )}
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => { setShowGoalForm(false); setGoalForm({ name: '', target_amount: '', deadline: '' }); setGoalFormError('') }}
                  className="flex-1 py-2 rounded-lg bg-gray-700 text-gray-300 hover:text-white text-sm transition-colors cursor-pointer"
                >
                  {t('actions.cancel')}
                </button>
                <button
                  type="button"
                  onClick={handleCreateGoal}
                  disabled={goalFormSaving}
                  className="flex-1 py-2 rounded-lg bg-yellow-400 hover:bg-yellow-300 text-gray-900 font-semibold text-sm transition-colors cursor-pointer disabled:opacity-60"
                >
                  {goalFormSaving ? t('saving') : t('actions.save')}
                </button>
              </div>
            </div>
          )}

          {!goalsLoading && !goalsError && goals.length === 0 && (
            <div className="text-center py-12">
              <div className="text-4xl mb-3">🎯</div>
              <p className="text-gray-400">{t('goals.noGoals')}</p>
            </div>
          )}

          {goals.map(goal => {
            const pct = goal.target_amount > 0 ? Math.min(100, (goal.current_amount / goal.target_amount) * 100) : 0
            const reached = goal.current_amount >= goal.target_amount
            const remaining = Math.max(0, goal.target_amount - goal.current_amount)
            return (
              <div key={goal.id} className="bg-gray-800 rounded-2xl p-5 space-y-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="flex items-center gap-3 min-w-0">
                    <Target size={24} className="text-yellow-400 shrink-0" />
                    <div className="min-w-0">
                      <p className="text-white font-semibold text-lg leading-tight truncate">{goal.name}</p>
                      <p className="text-gray-400 text-sm">
                        {formatAmount(goal.target_amount, goal.currency)}
                      </p>
                    </div>
                  </div>
                  {reached && (
                    <span className="shrink-0 text-green-400 text-sm font-semibold flex items-center gap-1">
                      <CheckCircle2 size={16} />
                      {t('goals.goalReached')}
                    </span>
                  )}
                </div>

                {/* Progress bar */}
                <div>
                  <div className="flex justify-between text-xs text-gray-400 mb-1">
                    <span>{formatAmount(goal.current_amount, goal.currency)} {t('goals.saved')}</span>
                    <span>{formatAmount(remaining, goal.currency)} {t('goals.remaining')}</span>
                  </div>
                  <div className="w-full bg-gray-700 rounded-full h-3 overflow-hidden">
                    <div
                      className={`h-3 rounded-full transition-all ${reached ? 'bg-green-400' : 'bg-yellow-400'}`}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  <div className="flex justify-between text-xs mt-1">
                    <span className="text-gray-500">{Math.round(pct)}%</span>
                    {!reached && goal.weeks_remaining != null && (
                      <span className="text-gray-400">
                        {goal.weeks_remaining < 1
                          ? t('goals.weeksRemainingLessThanOne')
                          : t('goals.weeksRemaining', { weeks: Math.ceil(goal.weeks_remaining) })}
                      </span>
                    )}
                    {goal.deadline && (
                      <span className="text-gray-500">
                        {formatDate(goal.deadline + 'T00:00:00Z', { month: 'short', day: 'numeric', timeZone: 'UTC' })}
                      </span>
                    )}
                  </div>
                </div>

                {/* Update saved amount */}
                {!reached && (
                  <div className="flex gap-2 pt-1">
                    <input
                      type="number"
                      min="0"
                      step="1"
                      aria-label={t('goals.updateSaved')}
                      value={savedInput[goal.id] ?? ''}
                      onChange={e => setSavedInput(prev => ({ ...prev, [goal.id]: e.target.value }))}
                      className="flex-1 bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-yellow-400"
                      placeholder={t('goals.currentAmount')}
                    />
                    <button
                      type="button"
                      onClick={() => handleUpdateSaved(goal.id)}
                      disabled={updatingSaved === goal.id || !savedInput[goal.id]}
                      className="px-4 py-2 bg-yellow-400 hover:bg-yellow-300 active:scale-95 text-gray-900 rounded-lg font-semibold text-sm transition-all cursor-pointer disabled:opacity-60 disabled:cursor-not-allowed"
                    >
                      {updatingSaved === goal.id ? t('saving') : t('actions.save')}
                    </button>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
