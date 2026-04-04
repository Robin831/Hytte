import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Camera, CheckCircle, XCircle, Plus, Pencil, Trash2, Star, X } from 'lucide-react'
import { formatDate, formatNumber } from '../utils/formatDate'
import { Skeleton } from '../components/ui/skeleton'
import { ConfirmDialog } from '../components/ui/dialog'
import { Tabs, TabList, TabTrigger, TabPanel } from '../components/ui/tabs'

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
  team_member_names?: string[]
  photo_url?: string
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
  completion_mode: 'solo' | 'team'
  min_team_size: number
  team_bonus_pct: number
}

interface Payout {
  id: number
  parent_id: number
  child_id: number
  child_nickname?: string
  child_avatar?: string
  week_start: string
  base_amount: number
  bonus_amount: number
  total_amount: number
  currency: string
  paid_out: boolean
  paid_at?: string
  created_at: string
}

interface Extra {
  id: number
  parent_id: number
  child_id: number | null
  name: string
  amount: number
  currency: string
  status: string
  claimed_by: number | null
  completed_at: string | null
  approved_at: string | null
  expires_at: string | null
  created_at: string
}

interface BonusRule {
  id: number
  parent_id: number
  type: string
  multiplier: number
  flat_amount: number
  active: boolean
}

type Tab = 'today' | 'chores' | 'payouts' | 'extras' | 'bonuses'

interface ChoreFormState {
  name: string
  amount: string
  frequency: string
  icon: string
  requires_approval: boolean
  completion_mode: 'solo' | 'team'
  min_team_size: string
  team_bonus_pct: string
}

const DEFAULT_CHORE_FORM: ChoreFormState = {
  name: '',
  amount: '',
  frequency: 'daily',
  icon: '🧹',
  requires_approval: true,
  completion_mode: 'solo',
  min_team_size: '2',
  team_bonus_pct: '10',
}

// Curated emoji sets for the chore icon picker
const CHORE_EMOJIS = [
  { key: 'cleaning', emojis: ['🧹', '🧽', '🧺', '🪣', '🫧', '🗑️'] },
  { key: 'kitchen', emojis: ['🍽️', '🧑‍🍳', '🥘', '🫕'] },
  { key: 'outdoors', emojis: ['🌿', '🪴', '🚗', '🐕', '🪵'] },
  { key: 'school', emojis: ['📚', '✏️', '🎒', '📐'] },
  { key: 'personal', emojis: ['🛁', '🪥', '👕', '🛏️'] },
  { key: 'general', emojis: ['⭐', '✅', '💪', '🏠', '🔧'] },
]

// Bonus types that use a multiplier vs. a flat amount
const MULTIPLIER_TYPES = new Set(['full_week', 'streak'])

const BONUS_TYPES = ['full_week', 'early_bird', 'streak', 'quality'] as const
type BonusType = (typeof BONUS_TYPES)[number]

const ACTIVE_BONUS_TYPES: BonusType[] = ['full_week', 'early_bird', 'streak', 'quality']


interface BonusRuleFormState {
  multiplier: string
  flat_amount: string
  active: boolean
}

export default function AllowancePage() {
  const { t } = useTranslation('allowance')
  const [tab, setTab] = useState<Tab>('today')

  // Today tab state
  const [pending, setPending] = useState<CompletionWithDetails[]>([])
  const [pendingLoading, setPendingLoading] = useState(true)
  const [pendingError, setPendingError] = useState('')
  const [photoPreviewId, setPhotoPreviewId] = useState<number | null>(null)
  const [photoEnlarged, setPhotoEnlarged] = useState(false)
  const enlargedCloseRef = useRef<HTMLButtonElement>(null)
  const enlargedTriggerRef = useRef<HTMLElement | null>(null)

  // Chores tab state
  const [chores, setChores] = useState<Chore[]>([])
  const [choresLoading, setChoresLoading] = useState(false)
  const [choresError, setChoresError] = useState('')
  const [showChoreForm, setShowChoreForm] = useState(false)
  const [editingChore, setEditingChore] = useState<Chore | null>(null)
  const [choreForm, setChoreForm] = useState<ChoreFormState>(DEFAULT_CHORE_FORM)
  const [choreFormSaving, setChoreFormSaving] = useState(false)
  const [showEmojiPicker, setShowEmojiPicker] = useState(false)
  const [deactivateConfirmChore, setDeactivateConfirmChore] = useState<Chore | null>(null)
  const emojiPickerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (showEmojiPicker) {
      emojiPickerRef.current?.focus()
    }
  }, [showEmojiPicker])

  useEffect(() => {
    if (photoEnlarged) {
      enlargedCloseRef.current?.focus()
      document.body.style.overflow = 'hidden'
    } else {
      enlargedTriggerRef.current?.focus()
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [photoEnlarged])

  // Payouts tab state
  const [payouts, setPayouts] = useState<Payout[]>([])
  const [payoutsLoading, setPayoutsLoading] = useState(false)
  const [payoutsError, setPayoutsError] = useState('')

  // Extras tab state
  const [extras, setExtras] = useState<Extra[]>([])
  const [extrasLoading, setExtrasLoading] = useState(false)
  const [extrasError, setExtrasError] = useState('')
  const [showExtraForm, setShowExtraForm] = useState(false)
  const [extraForm, setExtraForm] = useState({ name: '', amount: '', expires_at: '' })
  const [extraFormSaving, setExtraFormSaving] = useState(false)
  const [extraFormError, setExtraFormError] = useState('')
  const [extraActionError, setExtraActionError] = useState('')

  // Bonuses tab state
  const [bonusesLoading, setBonusesLoading] = useState(false)
  const [bonusesError, setBonusesError] = useState('')
  const [bonusForms, setBonusForms] = useState<Record<BonusType, BonusRuleFormState>>({
    full_week: { multiplier: '1.2', flat_amount: '0', active: false },
    early_bird: { multiplier: '1.0', flat_amount: '5', active: false },
    streak: { multiplier: '1.1', flat_amount: '0', active: false },
    quality: { multiplier: '1.0', flat_amount: '10', active: false },
  })
  const [bonusSaving, setBonusSaving] = useState<BonusType | null>(null)
  const [bonusActionError, setBonusActionError] = useState('')

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
        if (!cancelled) { setPending(data.pending ?? []); setPendingError('') }
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
        if (!cancelled) { setChores(data.chores ?? []); setChoresError('') }
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
        if (!cancelled) { setPayouts(data.payouts ?? []); setPayoutsError('') }
      })
      .catch(() => { if (!cancelled) setPayoutsError(t('errors.loadFailed')) })
      .finally(() => { if (!cancelled) setPayoutsLoading(false) })
    return () => { cancelled = true }
  }, [tab, t])

  useEffect(() => {
    if (tab !== 'extras') return
    let cancelled = false
    void (async () => {
      setExtrasLoading(true)
      try {
        const res = await fetch('/api/allowance/extras', { credentials: 'include' })
        const data: { extras: Extra[] } = await (res.ok ? res.json() : Promise.reject(res))
        if (!cancelled) { setExtras(data.extras ?? []); setExtrasError('') }
      } catch {
        if (!cancelled) setExtrasError(t('errors.loadFailed'))
      } finally {
        if (!cancelled) setExtrasLoading(false)
      }
    })()
    return () => { cancelled = true }
  }, [tab, t])

  useEffect(() => {
    if (tab !== 'bonuses') return
    let cancelled = false
    void (async () => {
      setBonusesLoading(true)
      try {
        const res = await fetch('/api/allowance/bonuses', { credentials: 'include' })
        const data: { bonus_rules: BonusRule[] } = await (res.ok ? res.json() : Promise.reject(res))
        if (!cancelled) {
          setBonusesError('')
          // Populate form state from loaded rules using functional update to avoid stale closure
          setBonusForms(prev => {
            const updatedForms = { ...prev }
            for (const rule of data.bonus_rules ?? []) {
              const ruleType = rule.type as BonusType
              if (BONUS_TYPES.includes(ruleType)) {
                updatedForms[ruleType] = {
                  multiplier: String(rule.multiplier ?? 1.0),
                  flat_amount: String(rule.flat_amount ?? 0),
                  active: rule.active,
                }
              }
            }
            return updatedForms
          })
        }
      } catch {
        if (!cancelled) setBonusesError(t('errors.loadFailed'))
      } finally {
        if (!cancelled) setBonusesLoading(false)
      }
    })()
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
    if (!choreForm.name.trim()) {
      setSaveError(t('errors.nameRequired'))
      return
    }
    const amount = parseFloat(choreForm.amount)
    if (isNaN(amount) || amount < 0) {
      setSaveError(t('errors.amountInvalid'))
      return
    }
    let minTeamSize: number | undefined
    let teamBonusPct: number | undefined
    if (choreForm.completion_mode !== 'solo') {
      minTeamSize = parseInt(choreForm.min_team_size, 10)
      if (!isFinite(minTeamSize) || minTeamSize < 2) {
        setSaveError(t('errors.minTeamSizeInvalid'))
        return
      }
      teamBonusPct = parseFloat(choreForm.team_bonus_pct)
      if (!isFinite(teamBonusPct) || teamBonusPct < 0 || teamBonusPct > 100) {
        setSaveError(t('errors.teamBonusPctInvalid'))
        return
      }
    }

    setChoreFormSaving(true)
    setSaveError('')
    try {
      const body: {
        name: string
        amount: number
        frequency: string
        icon: string
        requires_approval: boolean
        completion_mode: 'solo' | 'team'
        min_team_size?: number
        team_bonus_pct?: number
      } = {
        name: choreForm.name.trim(),
        amount,
        frequency: choreForm.frequency,
        icon: choreForm.icon || '🧹',
        requires_approval: choreForm.requires_approval,
        completion_mode: choreForm.completion_mode,
      }
      if (choreForm.completion_mode !== 'solo') {
        body.min_team_size = minTeamSize
        body.team_bonus_pct = teamBonusPct
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

  const handleCreateExtra = async () => {
    if (!extraForm.name.trim()) {
      setExtraFormError(t('errors.nameRequired'))
      return
    }
    const amount = parseFloat(extraForm.amount)
    if (isNaN(amount) || amount < 0) {
      setExtraFormError(t('errors.amountInvalid'))
      return
    }

    setExtraFormSaving(true)
    setExtraFormError('')
    try {
      const body: { name: string; amount: number; expires_at?: string } = {
        name: extraForm.name.trim(),
        amount,
      }
      if (extraForm.expires_at) {
        body.expires_at = extraForm.expires_at
      }
      const res = await fetch('/api/allowance/extras', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) throw new Error()
      const created: Extra = await res.json()
      setExtras(prev => [created, ...prev])
      setShowExtraForm(false)
      setExtraForm({ name: '', amount: '', expires_at: '' })
    } catch {
      setExtraFormError(t('errors.actionFailed'))
    } finally {
      setExtraFormSaving(false)
    }
  }

  const handleApproveExtra = async (id: number) => {
    setExtraActionError('')
    try {
      const res = await fetch(`/api/allowance/extras/${id}/approve`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error()
      const updated: Extra = await res.json()
      setExtras(prev => prev.map(e => (e.id === updated.id ? updated : e)))
    } catch {
      setExtraActionError(t('errors.actionFailed'))
    }
  }

  const handleSaveBonusRule = async (bonusType: BonusType) => {
    setBonusSaving(bonusType)
    setBonusActionError('')
    try {
      const form = bonusForms[bonusType]
      const multiplier = parseFloat(form.multiplier)
      const flatAmount = parseFloat(form.flat_amount)
      if (isNaN(multiplier) || multiplier < 1.0) {
        setBonusActionError(t('errors.amountInvalid'))
        return
      }
      if (isNaN(flatAmount) || flatAmount < 0) {
        setBonusActionError(t('errors.amountInvalid'))
        return
      }
      const res = await fetch('/api/allowance/bonuses', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          type: bonusType,
          multiplier,
          flat_amount: flatAmount,
          active: form.active,
        }),
      })
      if (!res.ok) throw new Error()
      const saved: BonusRule = await res.json()
      // Sync the form state from the saved rule so the UI reflects the persisted values
      setBonusForms(prev => ({
        ...prev,
        [bonusType]: {
          multiplier: String(saved.multiplier ?? 1.0),
          flat_amount: String(saved.flat_amount ?? 0),
          active: saved.active,
        },
      }))
    } catch {
      setBonusActionError(t('errors.actionFailed'))
    } finally {
      setBonusSaving(null)
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
      completion_mode: chore.completion_mode || 'solo',
      min_team_size: String(chore.min_team_size || 2),
      team_bonus_pct: String(chore.team_bonus_pct || 0),
    })
    setShowEmojiPicker(false)
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
    else if (newTab === 'extras') { setExtrasLoading(true); setExtrasError('') }
    else if (newTab === 'bonuses') { setBonusesLoading(true); setBonusesError('') }
    setShowEmojiPicker(false)
    setTab(newTab)
  }

  const tabs: { id: Tab; label: string }[] = [
    { id: 'today', label: t('tabs.today') },
    { id: 'chores', label: t('tabs.chores') },
    { id: 'extras', label: t('tabs.extras') },
    { id: 'bonuses', label: t('tabs.bonuses') },
    { id: 'payouts', label: t('tabs.payouts') },
  ]

  const extraStatusBadge = (status: string) => {
    const labels: Record<string, string> = {
      open: 'bg-blue-500/20 text-blue-300',
      claimed: 'bg-orange-500/20 text-orange-300',
      completed: 'bg-yellow-500/20 text-yellow-300',
      approved: 'bg-green-500/20 text-green-300',
      expired: 'bg-gray-500/20 text-gray-400',
    }
    return labels[status] ?? 'bg-gray-500/20 text-gray-400'
  }

  return (
    <div className="max-w-2xl mx-auto p-4 md:p-6">
      <h1 className="text-2xl font-bold text-white mb-6">{t('title')}</h1>

      {/* Tab bar */}
      <Tabs value={tab} onChange={(v) => handleTabSwitch(v as Tab)} variant="segment">
        <TabList aria-label={t('tabs.label')}>
          {tabs.map(({ id, label }) => (
            <TabTrigger key={id} value={id}>
              {label}
              {id === 'today' && pending.length > 0 && !pendingLoading && (
                <span className="ml-1.5 inline-flex items-center justify-center min-w-[18px] h-[18px] rounded-full bg-amber-500 text-white text-[10px] font-bold px-1">
                  {pending.length}
                </span>
              )}
            </TabTrigger>
          ))}
        </TabList>

      {/* Today — pending approvals */}
      <TabPanel value="today">
          {actionError && (
            <p className="text-red-400 text-sm mb-3">{actionError}</p>
          )}
          {pendingLoading ? (
            <div role="status" aria-live="polite">
              <span className="sr-only">{t('loading')}</span>
              <Skeleton className="h-5 w-32" />
            </div>
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
                <div key={comp.id} className="bg-gray-800 rounded-xl p-4 space-y-2">
                  <div className="flex items-center gap-4">
                    <div className="text-3xl select-none">{comp.child_avatar || '⭐'}</div>
                    <div className="flex-1 min-w-0">
                      <p className="text-xs text-gray-400 uppercase tracking-wide">
                        {(() => {
                          if (!Array.isArray(comp.team_member_names) || comp.team_member_names.length === 0) {
                            return comp.child_nickname
                          }

                          const cleanedNames = comp.team_member_names
                            .map(name => (name ?? '').trim())
                            .filter(name => name.length > 0)

                          if (cleanedNames.length === 0) {
                            return comp.child_nickname
                          }

                          return cleanedNames.join(t('teamMemberSeparator'))
                        })()}
                      </p>
                      <p className="text-white font-semibold">
                        {comp.chore_icon} {comp.chore_name}
                      </p>
                      <p className="text-sm text-gray-400">
                        {formatLocalDate(comp.date)} · {formatNumber(comp.chore_amount, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} {t('currency')}
                      </p>
                      {comp.notes && (
                        <p className="text-sm text-gray-300 mt-1 italic">"{comp.notes}"</p>
                      )}
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      {comp.photo_url && (
                        <button
                          type="button"
                          onClick={() => {
                            if (photoPreviewId === comp.id) {
                              setPhotoPreviewId(null)
                              setPhotoEnlarged(false)
                            } else {
                              setPhotoPreviewId(comp.id)
                              setPhotoEnlarged(false)
                            }
                          }}
                          aria-label={t('actions.viewPhoto')}
                          className="relative p-1.5 rounded-full text-blue-400 hover:bg-blue-500/20 transition-colors cursor-pointer"
                        >
                          <Camera size={24} />
                          <span className="absolute -top-0.5 -right-0.5 w-2.5 h-2.5 bg-blue-400 rounded-full" />
                        </button>
                      )}
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
                  {/* Photo thumbnail */}
                  {comp.photo_url && photoPreviewId === comp.id && (
                    <div className="mt-2">
                      <button
                        type="button"
                        onClick={e => { enlargedTriggerRef.current = e.currentTarget; setPhotoEnlarged(true) }}
                        className="block w-24 h-24 rounded-lg overflow-hidden cursor-pointer hover:opacity-90 transition-opacity"
                        aria-label={t('actions.viewPhoto')}
                      >
                        <img
                          src={comp.photo_url ?? `/api/allowance/photos/${comp.id}`}
                          alt={`${comp.chore_icon ?? ''} ${comp.chore_name}`.trim()}
                          className="w-full h-full object-cover"
                        />
                      </button>
                    </div>
                  )}
                </div>
              ))}
              {/* Enlarged photo overlay */}
              {photoEnlarged && photoPreviewId !== null && (
                <div
                  role="dialog"
                  aria-modal="true"
                  aria-label={t('actions.viewPhoto')}
                  className="fixed inset-0 z-50 bg-black/90 flex items-center justify-center p-4"
                  onClick={() => { setPhotoEnlarged(false) }}
                  onKeyDown={e => {
                    if (e.key === 'Escape') {
                      setPhotoEnlarged(false)
                    } else if (e.key === 'Tab') {
                      e.preventDefault()
                      enlargedCloseRef.current?.focus()
                    }
                  }}
                >
                  <button
                    ref={enlargedCloseRef}
                    type="button"
                    onClick={() => { setPhotoEnlarged(false) }}
                    aria-label={t('actions.close')}
                    className="absolute top-4 right-4 p-2 text-white bg-gray-800/80 rounded-full hover:bg-gray-700 transition-colors cursor-pointer"
                  >
                    <X size={20} />
                  </button>
                  <img
                    src={(() => { const c = pending.find(p => p.id === photoPreviewId); return c?.photo_url ?? `/api/allowance/photos/${photoPreviewId}` })()}
                    alt={(() => { const c = pending.find(p => p.id === photoPreviewId); return c ? `${c.chore_icon ?? ''} ${c.chore_name}`.trim() : t('actions.viewPhoto') })()}
                    className="max-w-full max-h-full object-contain rounded-lg"
                    onClick={e => e.stopPropagation()}
                  />
                </div>
              )}
            </div>
          )}
      </TabPanel>

      {/* Chores — manage definitions */}
      <TabPanel value="chores">
          <div className="flex justify-end mb-4">
            <button
              type="button"
              onClick={() => {
                setEditingChore(null)
                setChoreForm(DEFAULT_CHORE_FORM)
                setShowEmojiPicker(false)
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
                    <span className="block text-sm text-gray-400 mb-1">
                      {t('form.icon')}
                    </span>
                    <div className="relative">
                      <button
                        type="button"
                        onClick={() => setShowEmojiPicker(p => !p)}
                        onKeyDown={e => { if (e.key === 'Escape') setShowEmojiPicker(false) }}
                        className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-2xl text-center focus:outline-none focus:ring-2 focus:ring-blue-500 cursor-pointer"
                        aria-label={t('form.chooseIcon')}
                        aria-haspopup="dialog"
                        aria-expanded={showEmojiPicker}
                      >
                        {choreForm.icon}
                      </button>
                      {showEmojiPicker && (
                        <>
                          <div
                            className="fixed inset-0 z-10"
                            onClick={() => setShowEmojiPicker(false)}
                          />
                          <div
                            role="dialog"
                            aria-modal="true"
                            aria-label={t('form.chooseIcon')}
                            tabIndex={-1}
                            ref={emojiPickerRef}
                            onKeyDown={e => { if (e.key === 'Escape') setShowEmojiPicker(false) }}
                            className="absolute right-0 top-full mt-1 bg-gray-800 border border-gray-600 rounded-xl p-3 z-20 w-64 shadow-xl focus:outline-none"
                          >
                            {CHORE_EMOJIS.map(({ key, emojis }) => (
                              <div key={key} className="mb-3 last:mb-0">
                                <p className="text-xs text-gray-400 mb-1">{t(`form.emojiCategories.${key}` as never)}</p>
                                <div className="flex flex-wrap gap-1">
                                  {emojis.map(emoji => (
                                    <button
                                      key={emoji}
                                      type="button"
                                      onClick={() => {
                                        setChoreForm(f => ({ ...f, icon: emoji }))
                                        setShowEmojiPicker(false)
                                      }}
                                      className={`text-2xl p-1.5 rounded-lg transition-colors cursor-pointer ${choreForm.icon === emoji ? 'bg-blue-600' : 'hover:bg-gray-600'}`}
                                    >
                                      {emoji}
                                    </button>
                                  ))}
                                </div>
                              </div>
                            ))}
                            <div className="mt-2 border-t border-gray-600 pt-2">
                              <label htmlFor="chore-icon-custom" className="block text-xs text-gray-400 mb-1">{t('form.customEmoji')}</label>
                              <input
                                id="chore-icon-custom"
                                type="text"
                                value={choreForm.icon}
                                onChange={e => setChoreForm(f => ({ ...f, icon: e.target.value }))}
                                className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm text-center focus:outline-none focus:ring-2 focus:ring-blue-500"
                              />
                            </div>
                          </div>
                        </>
                      )}
                    </div>
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
                <div>
                  <label htmlFor="chore-completion-mode" className="block text-sm text-gray-400 mb-1">
                    {t('form.completionMode')}
                  </label>
                  <select
                    id="chore-completion-mode"
                    value={choreForm.completion_mode}
                    onChange={e => setChoreForm(f => ({ ...f, completion_mode: e.target.value as 'solo' | 'team' }))}
                    className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                  >
                    <option value="solo">{t('form.completionModeSolo')}</option>
                    <option value="team">{t('form.completionModeTeam')}</option>
                  </select>
                </div>
                {choreForm.completion_mode !== 'solo' && (
                  <div className="flex gap-3">
                    <div className="flex-1">
                      <label htmlFor="chore-min-team-size" className="block text-sm text-gray-400 mb-1">
                        {t('form.minTeamSize')}
                      </label>
                      <input
                        id="chore-min-team-size"
                        type="number"
                        min="2"
                        step="1"
                        value={choreForm.min_team_size}
                        onChange={e => setChoreForm(f => ({ ...f, min_team_size: e.target.value }))}
                        className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                        placeholder="2"
                      />
                    </div>
                    <div className="flex-1">
                      <label htmlFor="chore-team-bonus-pct" className="block text-sm text-gray-400 mb-1">
                        {t('form.teamBonusPct')}
                      </label>
                      <input
                        id="chore-team-bonus-pct"
                        type="number"
                        min="0"
                        max="100"
                        step="5"
                        value={choreForm.team_bonus_pct}
                        onChange={e => setChoreForm(f => ({ ...f, team_bonus_pct: e.target.value }))}
                        className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                        placeholder="0"
                      />
                    </div>
                  </div>
                )}
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
                    setShowEmojiPicker(false)
                  }}
                  className="flex-1 py-2 rounded-lg bg-gray-700 text-gray-300 hover:text-white text-sm transition-colors cursor-pointer"
                >
                  {t('actions.cancel')}
                </button>
                <button
                  type="button"
                  onClick={handleSaveChore}
                  disabled={
                    choreFormSaving ||
                    !choreForm.name.trim() ||
                    choreForm.amount === '' ||
                    Number.isNaN(Number(choreForm.amount)) ||
                    Number(choreForm.amount) < 0
                  }
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
            <div role="status" aria-live="polite">
              <span className="sr-only">{t('loading')}</span>
              <Skeleton className="h-5 w-32" />
            </div>
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
                    <p className="text-white font-medium">
                      {chore.name}
                      {chore.completion_mode === 'team' && (
                        <span className="ml-1.5" title={t('form.completionModeTeam')}>🤝</span>
                      )}
                    </p>
                    <p className="text-sm text-gray-400">
                      {formatNumber(chore.amount, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} {t('currency')} · {t(`frequency.${chore.frequency}` as never)}
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
                        onClick={() => setDeactivateConfirmChore(chore)}
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
      </TabPanel>

      {/* Extras — one-off tasks */}
      <TabPanel value="extras">
          <div className="flex justify-end mb-4">
            <button
              type="button"
              onClick={() => {
                setShowExtraForm(true)
                setExtraFormError('')
              }}
              className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm font-medium transition-colors cursor-pointer"
            >
              <Plus size={16} />
              {t('actions.addExtra')}
            </button>
          </div>

          {showExtraForm && (
            <div className="bg-gray-800 rounded-xl p-4 mb-4">
              <h3 className="text-white font-medium mb-4">{t('form.newExtra')}</h3>
              <div className="space-y-3">
                <div>
                  <label htmlFor="extra-name" className="block text-sm text-gray-400 mb-1">
                    {t('form.extraName')}
                  </label>
                  <input
                    id="extra-name"
                    type="text"
                    value={extraForm.name}
                    onChange={e => setExtraForm(f => ({ ...f, name: e.target.value }))}
                    className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                    placeholder={t('form.extraNamePlaceholder')}
                  />
                </div>
                <div className="flex gap-3">
                  <div className="flex-1">
                    <label htmlFor="extra-amount" className="block text-sm text-gray-400 mb-1">
                      {t('form.amount')} ({t('currency')})
                    </label>
                    <input
                      id="extra-amount"
                      type="number"
                      min="0"
                      step="0.5"
                      value={extraForm.amount}
                      onChange={e => setExtraForm(f => ({ ...f, amount: e.target.value }))}
                      className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                      placeholder="10"
                    />
                  </div>
                  <div className="flex-1">
                    <label htmlFor="extra-expires" className="block text-sm text-gray-400 mb-1">
                      {t('form.expiresAt')}
                    </label>
                    <input
                      id="extra-expires"
                      type="date"
                      value={extraForm.expires_at}
                      onChange={e => setExtraForm(f => ({ ...f, expires_at: e.target.value }))}
                      className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                    />
                  </div>
                </div>
              </div>
              {extraFormError && (
                <p className="text-red-400 text-sm mt-3">{extraFormError}</p>
              )}
              <div className="flex gap-2 mt-4">
                <button
                  type="button"
                  onClick={() => {
                    setShowExtraForm(false)
                    setExtraForm({ name: '', amount: '', expires_at: '' })
                    setExtraFormError('')
                  }}
                  className="flex-1 py-2 rounded-lg bg-gray-700 text-gray-300 hover:text-white text-sm transition-colors cursor-pointer"
                >
                  {t('actions.cancel')}
                </button>
                <button
                  type="button"
                  onClick={handleCreateExtra}
                  disabled={
                    extraFormSaving ||
                    !extraForm.name.trim() ||
                    extraForm.amount === '' ||
                    Number.isNaN(Number(extraForm.amount)) ||
                    Number(extraForm.amount) < 0
                  }
                  className="flex-1 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {extraFormSaving ? t('saving') : t('actions.save')}
                </button>
              </div>
            </div>
          )}

          {extraActionError && (
            <p className="text-red-400 text-sm mb-3">{extraActionError}</p>
          )}
          {extrasLoading ? (
            <div role="status" aria-live="polite">
              <span className="sr-only">{t('loading')}</span>
              <Skeleton className="h-5 w-32" />
            </div>
          ) : extrasError ? (
            <p className="text-red-400 text-sm">{extrasError}</p>
          ) : extras.length === 0 ? (
            <div className="text-center py-16 text-gray-400">
              <p className="text-5xl mb-4">🎯</p>
              <p className="text-sm">{t('noExtras')}</p>
            </div>
          ) : (
            <div className="space-y-3">
              {extras.map(extra => (
                <div key={extra.id} className="bg-gray-800 rounded-xl p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1">
                        <p className="text-white font-semibold">{extra.name}</p>
                        <span className={`text-xs px-2 py-0.5 rounded-full font-medium ${extraStatusBadge(extra.status)}`}>
                          {t(`extras.status.${extra.status}` as never, { defaultValue: extra.status })}
                        </span>
                      </div>
                      <p className="text-yellow-400 font-bold">{formatNumber(extra.amount, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} {t('currency')}</p>
                      {extra.expires_at && (
                        <p className="text-xs text-gray-500 mt-1">
                          {formatLocalDate(extra.expires_at.split('T')[0])}
                        </p>
                      )}
                    </div>
                    {(extra.status === 'claimed' || extra.status === 'completed') && (
                      <button
                        type="button"
                        onClick={() => handleApproveExtra(extra.id)}
                        className="shrink-0 flex items-center gap-1.5 px-3 py-1.5 bg-green-600/20 hover:bg-green-600/40 text-green-400 rounded-lg text-sm font-medium transition-colors cursor-pointer"
                      >
                        <CheckCircle size={14} />
                        {t('actions.approveExtra')}
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
      </TabPanel>

      {/* Bonuses — bonus rules settings */}
      <TabPanel value="bonuses">
          {bonusActionError && (
            <p className="text-red-400 text-sm mb-3">{bonusActionError}</p>
          )}
          {bonusesLoading ? (
            <div role="status" aria-live="polite">
              <span className="sr-only">{t('loading')}</span>
              <Skeleton className="h-5 w-32" />
            </div>
          ) : bonusesError ? (
            <p className="text-red-400 text-sm">{bonusesError}</p>
          ) : (
            <div className="space-y-4">
              {ACTIVE_BONUS_TYPES.map(bonusType => {
                const form = bonusForms[bonusType]
                const isSaving = bonusSaving === bonusType
                const isMultiplierType = MULTIPLIER_TYPES.has(bonusType)
                return (
                  <div key={bonusType} className="bg-gray-800 rounded-xl p-4">
                    <div className="flex items-start justify-between mb-3">
                      <div className="flex-1">
                        <div className="flex items-center gap-2 mb-1">
                          <Star size={16} className="text-yellow-400 shrink-0" />
                          <h3 className="text-white font-semibold">
                            {t(`bonuses.${bonusType}` as never, { defaultValue: bonusType })}
                          </h3>
                        </div>
                        <p className="text-gray-400 text-sm">
                          {t(`bonuses.${bonusType}_desc` as never, { defaultValue: '' })}
                        </p>
                      </div>
                      <div className="flex items-center gap-2 shrink-0 ml-3">
                        <label htmlFor={`bonus-active-${bonusType}`} className="text-sm text-gray-400">
                          {t('bonuses.active')}
                        </label>
                        <input
                          id={`bonus-active-${bonusType}`}
                          type="checkbox"
                          checked={form.active}
                          onChange={e =>
                            setBonusForms(prev => ({
                              ...prev,
                              [bonusType]: { ...prev[bonusType], active: e.target.checked },
                            }))
                          }
                          className="w-4 h-4 rounded accent-yellow-400"
                        />
                      </div>
                    </div>

                    <div className="flex gap-3">
                      {isMultiplierType ? (
                        <div className="flex-1">
                          <label
                            htmlFor={`bonus-multiplier-${bonusType}`}
                            className="block text-xs text-gray-400 mb-1"
                          >
                            {t('bonuses.multiplier')}
                          </label>
                          <input
                            id={`bonus-multiplier-${bonusType}`}
                            type="number"
                            min="1"
                            step="0.05"
                            value={form.multiplier}
                            onChange={e =>
                              setBonusForms(prev => ({
                                ...prev,
                                [bonusType]: { ...prev[bonusType], multiplier: e.target.value },
                              }))
                            }
                            className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-yellow-500"
                          />
                        </div>
                      ) : (
                        <div className="flex-1">
                          <label
                            htmlFor={`bonus-flat-${bonusType}`}
                            className="block text-xs text-gray-400 mb-1"
                          >
                            {t('bonuses.flatAmount')}
                          </label>
                          <input
                            id={`bonus-flat-${bonusType}`}
                            type="number"
                            min="0"
                            step="1"
                            value={form.flat_amount}
                            onChange={e =>
                              setBonusForms(prev => ({
                                ...prev,
                                [bonusType]: { ...prev[bonusType], flat_amount: e.target.value },
                              }))
                            }
                            className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-yellow-500"
                          />
                        </div>
                      )}
                      <div className="flex items-end">
                        <button
                          type="button"
                          onClick={() => handleSaveBonusRule(bonusType)}
                          disabled={isSaving}
                          className="px-3 py-2 bg-yellow-500/20 hover:bg-yellow-500/40 text-yellow-400 rounded-lg text-sm font-medium transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          {isSaving ? t('saving') : t('actions.saveRule')}
                        </button>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
      </TabPanel>

      {/* Payouts — weekly summaries */}
      <TabPanel value="payouts">
          {payoutActionError && (
            <p className="text-red-400 text-sm mb-3">{payoutActionError}</p>
          )}
          {payoutsLoading ? (
            <div role="status" aria-live="polite">
              <span className="sr-only">{t('loading')}</span>
              <Skeleton className="h-5 w-32" />
            </div>
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
                    <div className="flex items-center gap-2">
                      <span className="text-xl select-none">{payout.child_avatar || '⭐'}</span>
                      <div>
                        <p className="text-sm font-medium text-white">{payout.child_nickname}</p>
                        <p className="text-xs text-gray-400">{formatWeekRange(payout.week_start)}</p>
                      </div>
                    </div>
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
                    <p className="text-white font-bold text-xl">{formatNumber(payout.total_amount, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} {t('currency')}</p>
                    <div className="text-sm text-gray-400 text-right space-y-0.5">
                      {payout.base_amount > 0 && (
                        <p>{t('breakdown.base')}: {formatNumber(payout.base_amount, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} {t('currency')}</p>
                      )}
                      {payout.bonus_amount > 0 && (
                        <p>{t('breakdown.bonus')}: +{formatNumber(payout.bonus_amount, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} {t('currency')}</p>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
      </TabPanel>
      </Tabs>
      <ConfirmDialog
        open={deactivateConfirmChore !== null}
        onClose={() => setDeactivateConfirmChore(null)}
        onConfirm={() => deactivateConfirmChore && handleDeactivateChore(deactivateConfirmChore.id)}
        title={t('actions.deactivate')}
        message={deactivateConfirmChore ? t('confirmDeactivate', { name: deactivateConfirmChore.name }) : undefined}
      />
    </div>
  )
}
