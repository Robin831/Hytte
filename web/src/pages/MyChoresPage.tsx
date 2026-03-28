import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, Clock, XCircle, Coins } from 'lucide-react'

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

type Tab = 'chores' | 'earnings' | 'extras'

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
      <div className="flex gap-2 bg-gray-800 rounded-xl p-1">
        <button
          onClick={() => setTab('chores')}
          className={`flex-1 py-2 rounded-lg text-sm font-semibold transition-colors cursor-pointer ${
            tab === 'chores'
              ? 'bg-yellow-400 text-gray-900'
              : 'text-gray-400 hover:text-white'
          }`}
        >
          {t('myChores.tabs.chores')}
        </button>
        <button
          onClick={() => setTab('extras')}
          className={`flex-1 py-2 rounded-lg text-sm font-semibold transition-colors cursor-pointer ${
            tab === 'extras'
              ? 'bg-yellow-400 text-gray-900'
              : 'text-gray-400 hover:text-white'
          }`}
        >
          {t('myChores.tabs.extras')}
        </button>
        <button
          onClick={() => setTab('earnings')}
          className={`flex-1 py-2 rounded-lg text-sm font-semibold transition-colors cursor-pointer ${
            tab === 'earnings'
              ? 'bg-yellow-400 text-gray-900'
              : 'text-gray-400 hover:text-white'
          }`}
        >
          {t('myChores.tabs.earnings')}
        </button>
      </div>

      {/* Chores tab */}
      {tab === 'chores' && (
        <div className="space-y-4">
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
        <div className="space-y-4">
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
              {extras.map(extra => (
                <div
                  key={extra.id}
                  className="bg-gray-800 rounded-2xl p-5 flex items-center gap-4"
                >
                  <span className="text-4xl select-none shrink-0">🎯</span>
                  <div className="flex-1 min-w-0">
                    <p className="text-white font-semibold text-lg leading-tight">{extra.name}</p>
                    <p className="text-yellow-400 font-bold text-xl mt-1">
                      {formatAmount(extra.amount, extra.currency)}
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
              ))}
            </div>
          )}
        </div>
      )}

      {/* Earnings tab */}
      {tab === 'earnings' && (
        <div className="space-y-4">
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
    </div>
  )
}
