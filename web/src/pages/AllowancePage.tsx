import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle, XCircle, Plus, Pencil, Trash2 } from 'lucide-react'
import { formatDate } from '../utils/formatDate'

interface CompletionWithDetails {
  id: number
  chore_id: number
  chore_name: string
  chore_icon: string
  chore_amount: number
  child_id: number
  child_nickname: string
  child_avatar: string
  date: string
  status: string
  notes?: string
  quality_bonus?: number
  created_at: string
}

interface Chore {
  id: number
  parent_id: number
  child_id: number | null
  name: string
  description: string
  amount: number
  currency: string
  frequency: string
  icon: string
  requires_approval: boolean
  active: boolean
  created_at: string
}

interface Payout {
  id: number
  parent_id: number
  child_id: number
  week_start: string
  base_amount: number
  bonus_amount: number
  total_amount: number
  currency: string
  paid_out: boolean
  paid_at?: string
  created_at: string
}

type Tab = 'today' | 'chores' | 'payouts'

interface ChoreFormState {
  name: string
  amount: string
  frequency: string
  icon: string
  requires_approval: boolean
  description: string
}

const DEFAULT_CHORE_FORM: ChoreFormState = {
  name: '',
  amount: '',
  frequency: 'daily',
  icon: '🧹',
  requires_approval: true,
  description: '',
}

export default function AllowancePage() {
  const { t } = useTranslation('allowance')
  const [tab, setTab] = useState<Tab>('today')

  // Today tab state
  const [pending, setPending] = useState<CompletionWithDetails[]>([])
  const [pendingLoading, setPendingLoading] = useState(true)
  const [pendingError, setPendingError] = useState('')

  // Chores tab state
  const [chores, setChores] = useState<Chore[]>([])
  const [choresLoading, setChoresLoading] = useState(false)
  const [choresError, setChoresError] = useState('')
  const [showChoreForm, setShowChoreForm] = useState(false)
  const [editingChore, setEditingChore] = useState<Chore | null>(null)
  const [choreForm, setChoreForm] = useState<ChoreFormState>(DEFAULT_CHORE_FORM)
  const [choreFormSaving, setChoreFormSaving] = useState(false)

  // Payouts tab state
  const [payouts, setPayouts] = useState<Payout[]>([])
  const [payoutsLoading, setPayoutsLoading] = useState(false)
  const [payoutsError, setPayoutsError] = useState('')

  // Action error feedback
  const [actionError, setActionError] = useState('')
  const [saveError, setSaveError] = useState('')
  const [deactivateError, setDeactivateError] = useState('')
  const [payoutActionError, setPayoutActionError] = useState('')

  useEffect(() => {
    if (tab !== 'today') return
    let cancelled = false
    fetch('/api/allowance/pending', { credentials: 'include' })
      .then(res => (res.ok ? res.json() : Promise.reject(res)))
      .then((data: { pending: CompletionWithDetails[] }) => {
        if (!cancelled) setPending(data.pending ?? [])
      })
      .catch(() => { if (!cancelled) setPendingError(t('errors.loadFailed')) })
      .finally(() => { if (!cancelled) setPendingLoading(false) })
    return () => { cancelled = true }
  }, [tab, t])

  useEffect(() => {
    if (tab !== 'chores') return
    let cancelled = false
    fetch('/api/allowance/chores', { credentials: 'include' })
      .then(res => (res.ok ? res.json() : Promise.reject(res)))
      .then((data: { chores: Chore[] }) => {
        if (!cancelled) setChores(data.chores ?? [])
      })
      .catch(() => { if (!cancelled) setChoresError(t('errors.loadFailed')) })
      .finally(() => { if (!cancelled) setChoresLoading(false) })
    return () => { cancelled = true }
  }, [tab, t])

  useEffect(() => {
    if (tab !== 'payouts') return
    let cancelled = false
    fetch('/api/allowance/payouts?weeks=8', { credentials: 'include' })
      .then(res => (res.ok ? res.json() : Promise.reject(res)))
      .then((data: { payouts: Payout[] }) => {
        if (!cancelled) setPayouts(data.payouts ?? [])
      })
      .catch(() => { if (!cancelled) setPayoutsError(t('errors.loadFailed')) })
      .finally(() => { if (!cancelled) setPayoutsLoading(false) })
    return () => { cancelled = true }
  }, [tab, t])

  const handleApprove = async (id: number) => {
    setActionError('')
    try {
      const res = await fetch(`/api/allowance/approve/${id}`, { method: 'POST', credentials: 'include' })
      if (!res.ok) throw new Error()
      setPending(prev => prev.filter(c => c.id !== id))
    } catch {
      setActionError(t('errors.actionFailed'))
    }
  }

  const handleReject = async (id: number) => {
    setActionError('')
    try {
      const res = await fetch(`/api/allowance/reject/${id}`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ reason: '' }),
      })
      if (!res.ok) throw new Error()
      setPending(prev => prev.filter(c => c.id !== id))
    } catch {
      setActionError(t('errors.actionFailed'))
    }
  }

  const handleSaveChore = async () => {
    if (!choreForm.name.trim()) return
    const amount = parseFloat(choreForm.amount)
    if (isNaN(amount) || amount < 0) return

    setChoreFormSaving(true)
    setSaveError('')
    try {
      const body = {
        name: choreForm.name.trim(),
        description: choreForm.description.trim(),
        amount,
        frequency: choreForm.frequency,
        icon: choreForm.icon || '🧹',
        requires_approval: choreForm.requires_approval,
      }
      const url = editingChore ? `/api/allowance/chores/${editingChore.id}` : '/api/allowance/chores'
      const method = editingChore ? 'PUT' : 'POST'
      const res = await fetch(url, {
        method,
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) throw new Error()
      const saved: Chore = await res.json()
      if (editingChore) {
        setChores(prev => prev.map(c => (c.id === saved.id ? saved : c)))
      } else {
        setChores(prev => [...prev, saved])
      }
      setShowChoreForm(false)
      setEditingChore(null)
      setChoreForm(DEFAULT_CHORE_FORM)
    } catch {
      setSaveError(t('errors.actionFailed'))
    } finally {
      setChoreFormSaving(false)
    }
  }

  const handleDeactivateChore = async (id: number) => {
    setDeactivateError('')
    try {
      const res = await fetch(`/api/allowance/chores/${id}`, { method: 'DELETE', credentials: 'include' })
      if (!res.ok) throw new Error()
      setChores(prev => prev.map(c => (c.id === id ? { ...c, active: false } : c)))
    } catch {
      setDeactivateError(t('errors.actionFailed'))
    }
  }

  const handleMarkPaid = async (id: number) => {
    setPayoutActionError('')
    try {
      const res = await fetch(`/api/allowance/payouts/${id}/paid`, { method: 'POST', credentials: 'include' })
      if (!res.ok) throw new Error()
      setPayouts(prev => prev.map(p => (p.id === id ? { ...p, paid_out: true } : p)))
    } catch {
      setPayoutActionError(t('errors.actionFailed'))
    }
  }

  const startEditChore = (chore: Chore) => {
    setEditingChore(chore)
    setChoreForm({
      name: chore.name,
      amount: String(chore.amount),
      frequency: chore.frequency,
      icon: chore.icon,
      requires_approval: chore.requires_approval,
      description: chore.description,
    })
    setShowChoreForm(true)
  }

  const formatLocalDate = (dateStr: string) =>
    formatDate(dateStr + 'T00:00:00', { weekday: 'short', month: 'short', day: 'numeric' })

  const formatWeekRange = (weekStart: string) => {
    try {
      const start = new Date(weekStart + 'T00:00:00')
      const end = new Date(weekStart + 'T00:00:00')
      end.setDate(start.getDate() + 6)
      const startStr = formatDate(start, { month: 'short', day: 'numeric' })
      const endStr = formatDate(end, { month: 'short', day: 'numeric' })
      return `${startStr} – ${endStr}`
    } catch {
      return weekStart
    }
  }

  const handleTabSwitch = (newTab: Tab) => {
    if (newTab === tab) return
    if (newTab === 'today') { setPendingLoading(true); setPendingError('') }
    else if (newTab === 'chores') { setChoresLoading(true); setChoresError('') }
    else if (newTab === 'payouts') { setPayoutsLoading(true); setPayoutsError('') }
    setTab(newTab)
  }

  const tabs: { id: Tab; label: string }[] = [
    { id: 'today', label: t('tabs.today') },
    { id: 'chores', label: t('tabs.chores') },
    { id: 'payouts', label: t('tabs.payouts') },
  ]

  return (
    <div className="max-w-2xl mx-auto p-4 md:p-6">
      <h1 className="text-2xl font-bold text-white mb-6">{t('title')}</h1>

      {/* Tab bar */}
      <div className="flex gap-1 mb-6 bg-gray-800 rounded-lg p-1">
        {tabs.map(({ id, label }) => (
          <button
            key={id}
            type="button"
            onClick={() => handleTabSwitch(id)}
            className={`flex-1 py-2 px-3 rounded-md text-sm font-medium transition-colors cursor-pointer ${
              tab === id ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'
            }`}
          >
            {label}
            {id === 'today' && pending.length > 0 && !pendingLoading && (
              <span className="ml-1.5 inline-flex items-center justify-center min-w-[18px] h-[18px] rounded-full bg-amber-500 text-white text-[10px] font-bold px-1">
                {pending.length}
              </span>
            )}
          </button>
        ))}
      </div>

      {/* Today — pending approvals */}
      {tab === 'today' && (
        <div>
          {actionError && (
            <p className="text-red-400 text-sm mb-3">{actionError}</p>
          )}
          {pendingLoading ? (
            <p className="text-gray-400 text-sm">{t('loading')}</p>
          ) : pendingError ? (
            <p className="text-red-400 text-sm">{pendingError}</p>
          ) : pending.length === 0 ? (
            <div className="text-center py-16 text-gray-400">
              <p className="text-5xl mb-4">✅</p>
              <p className="text-sm">{t('noPending')}</p>
            </div>
          ) : (
            <div className="space-y-3">
              {pending.map(comp => (
                <div key={comp.id} className="bg-gray-800 rounded-xl p-4 flex items-center gap-4">
                  <div className="text-3xl select-none">{comp.child_avatar || '⭐'}</div>
                  <div className="flex-1 min-w-0">
                    <p className="text-xs text-gray-400 uppercase tracking-wide">{comp.child_nickname}</p>
                    <p className="text-white font-semibold">
                      {comp.chore_icon} {comp.chore_name}
                    </p>
                    <p className="text-sm text-gray-400">
                      {formatLocalDate(comp.date)} · {comp.chore_amount} {t('currency')}
                    </p>
                    {comp.notes && (
                      <p className="text-sm text-gray-300 mt-1 italic">"{comp.notes}"</p>
                    )}
                  </div>
                  <div className="flex gap-2 shrink-0">
                    <button
                      type="button"
                      onClick={() => handleReject(comp.id)}
                      className="p-1.5 rounded-full text-red-400 hover:bg-red-500/20 transition-colors cursor-pointer"
                      aria-label={t('actions.reject')}
                    >
                      <XCircle size={32} />
                    </button>
                    <button
                      type="button"
                      onClick={() => handleApprove(comp.id)}
                      className="p-1.5 rounded-full text-green-400 hover:bg-green-500/20 transition-colors cursor-pointer"
                      aria-label={t('actions.approve')}
                    >
                      <CheckCircle size={32} />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Chores — manage definitions */}
      {tab === 'chores' && (
        <div>
          <div className="flex justify-end mb-4">
            <button
              type="button"
              onClick={() => {
                setEditingChore(null)
                setChoreForm(DEFAULT_CHORE_FORM)
                setShowChoreForm(true)
              }}
              className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm font-medium transition-colors cursor-pointer"
            >
              <Plus size={16} />
              {t('actions.addChore')}
            </button>
          </div>

          {showChoreForm && (
            <div className="bg-gray-800 rounded-xl p-4 mb-4">
              <h3 className="text-white font-medium mb-4">
                {editingChore ? t('form.editChore') : t('form.newChore')}
              </h3>
              <div className="space-y-3">
                <div className="flex gap-3">
                  <div className="flex-1">
                    <label htmlFor="chore-name" className="block text-sm text-gray-400 mb-1">
                      {t('form.name')}
                    </label>
                    <input
                      id="chore-name"
                      type="text"
                      value={choreForm.name}
                      onChange={e => setChoreForm(f => ({ ...f, name: e.target.value }))}
                      className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                      placeholder={t('form.namePlaceholder')}
                    />
                  </div>
                  <div className="w-20">
                    <label htmlFor="chore-icon" className="block text-sm text-gray-400 mb-1">
                      {t('form.icon')}
                    </label>
                    <input
                      id="chore-icon"
                      type="text"
                      value={choreForm.icon}
                      onChange={e => setChoreForm(f => ({ ...f, icon: e.target.value }))}
                      className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm text-center focus:outline-none focus:ring-2 focus:ring-blue-500"
                    />
                  </div>
                </div>
                <div className="flex gap-3">
                  <div className="flex-1">
                    <label htmlFor="chore-amount" className="block text-sm text-gray-400 mb-1">
                      {t('form.amount')} ({t('currency')})
                    </label>
                    <input
                      id="chore-amount"
                      type="number"
                      min="0"
                      step="0.5"
                      value={choreForm.amount}
                      onChange={e => setChoreForm(f => ({ ...f, amount: e.target.value }))}
                      className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                      placeholder="5"
                    />
                  </div>
                  <div className="flex-1">
                    <label htmlFor="chore-frequency" className="block text-sm text-gray-400 mb-1">
                      {t('form.frequency')}
                    </label>
                    <select
                      id="chore-frequency"
                      value={choreForm.frequency}
                      onChange={e => setChoreForm(f => ({ ...f, frequency: e.target.value }))}
                      className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                    >
                      <option value="daily">{t('frequency.daily')}</option>
                      <option value="weekly">{t('frequency.weekly')}</option>
                      <option value="once">{t('frequency.once')}</option>
                    </select>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <input
                    id="chore-requires-approval"
                    type="checkbox"
                    checked={choreForm.requires_approval}
                    onChange={e => setChoreForm(f => ({ ...f, requires_approval: e.target.checked }))}
                    className="w-4 h-4 rounded accent-blue-500"
                  />
                  <label htmlFor="chore-requires-approval" className="text-sm text-gray-300">
                    {t('form.requiresApproval')}
                  </label>
                </div>
              </div>
              {saveError && (
                <p className="text-red-400 text-sm mt-3">{saveError}</p>
              )}
              <div className="flex gap-2 mt-4">
                <button
                  type="button"
                  onClick={() => {
                    setShowChoreForm(false)
                    setEditingChore(null)
                    setSaveError('')
                  }}
                  className="flex-1 py-2 rounded-lg bg-gray-700 text-gray-300 hover:text-white text-sm transition-colors cursor-pointer"
                >
                  {t('actions.cancel')}
                </button>
                <button
                  type="button"
                  onClick={handleSaveChore}
                  disabled={choreFormSaving || !choreForm.name.trim()}
                  className="flex-1 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {choreFormSaving ? t('saving') : t('actions.save')}
                </button>
              </div>
            </div>
          )}

          {deactivateError && (
            <p className="text-red-400 text-sm mb-3">{deactivateError}</p>
          )}
          {choresLoading ? (
            <p className="text-gray-400 text-sm">{t('loading')}</p>
          ) : choresError ? (
            <p className="text-red-400 text-sm">{choresError}</p>
          ) : chores.length === 0 ? (
            <div className="text-center py-16 text-gray-400">
              <p className="text-5xl mb-4">📋</p>
              <p className="text-sm">{t('noChores')}</p>
            </div>
          ) : (
            <div className="space-y-2">
              {chores.map(chore => (
                <div
                  key={chore.id}
                  className={`bg-gray-800 rounded-xl p-4 flex items-center gap-3 ${!chore.active ? 'opacity-40' : ''}`}
                >
                  <span className="text-2xl select-none">{chore.icon}</span>
                  <div className="flex-1 min-w-0">
                    <p className="text-white font-medium">{chore.name}</p>
                    <p className="text-sm text-gray-400">
                      {chore.amount} {t('currency')} · {t(`frequency.${chore.frequency}` as never)}
                      {!chore.active && (
                        <span className="ml-2 text-gray-500">({t('inactive')})</span>
                      )}
                    </p>
                  </div>
                  {chore.active && (
                    <div className="flex gap-1 shrink-0">
                      <button
                        type="button"
                        onClick={() => startEditChore(chore)}
                        className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors cursor-pointer"
                        aria-label={t('actions.edit')}
                      >
                        <Pencil size={16} />
                      </button>
                      <button
                        type="button"
                        onClick={() => handleDeactivateChore(chore.id)}
                        className="p-2 text-gray-400 hover:text-red-400 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer"
                        aria-label={t('actions.deactivate')}
                      >
                        <Trash2 size={16} />
                      </button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Payouts — weekly summaries */}
      {tab === 'payouts' && (
        <div>
          {payoutActionError && (
            <p className="text-red-400 text-sm mb-3">{payoutActionError}</p>
          )}
          {payoutsLoading ? (
            <p className="text-gray-400 text-sm">{t('loading')}</p>
          ) : payoutsError ? (
            <p className="text-red-400 text-sm">{payoutsError}</p>
          ) : payouts.length === 0 ? (
            <div className="text-center py-16 text-gray-400">
              <p className="text-5xl mb-4">💰</p>
              <p className="text-sm">{t('noPayouts')}</p>
            </div>
          ) : (
            <div className="space-y-3">
              {payouts.map(payout => (
                <div key={payout.id} className="bg-gray-800 rounded-xl p-4">
                  <div className="flex items-center justify-between mb-2">
                    <p className="text-sm text-gray-400">{formatWeekRange(payout.week_start)}</p>
                    {payout.paid_out ? (
                      <span className="text-xs text-green-400 font-medium px-2 py-0.5 bg-green-500/10 rounded-full">
                        {t('paid')}
                      </span>
                    ) : (
                      <button
                        type="button"
                        onClick={() => handleMarkPaid(payout.id)}
                        className="text-xs px-2 py-1 rounded-md bg-green-600/20 text-green-400 hover:bg-green-600/40 transition-colors cursor-pointer"
                      >
                        {t('actions.markPaid')}
                      </button>
                    )}
                  </div>
                  <div className="flex items-baseline justify-between">
                    <p className="text-white font-bold text-xl">{payout.total_amount} {t('currency')}</p>
                    <div className="text-sm text-gray-400 text-right space-y-0.5">
                      {payout.base_amount > 0 && (
                        <p>{t('breakdown.base')}: {payout.base_amount} {t('currency')}</p>
                      )}
                      {payout.bonus_amount > 0 && (
                        <p>{t('breakdown.bonus')}: +{payout.bonus_amount} {t('currency')}</p>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
