import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Camera, CheckCircle2, Clock, XCircle, Coins, Target, Plus, Users } from 'lucide-react'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import { formatDate } from '../utils/formatDate'
import Confetti from '../components/Confetti'
import { Skeleton } from '../components/ui/skeleton'

interface ActiveTeamSession {
  completion_id: number
  participant_count: number
  participant_ids: number[]
  current_child_joined: boolean
}

interface SiblingInfo {
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface ChoreWithStatus {
  id: number
  name: string
  description: string
  amount: number
  currency: string
  frequency: string
  icon: string
  requires_approval: boolean
  completion_mode: string
  min_team_size: number
  team_bonus_pct: number
  completion_id?: number
  completion_status?: string
  active_team_session?: ActiveTeamSession
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

interface AllowanceBingoCell {
  challenge_key: string
  label: string
  completed: boolean
}

interface AllowanceBingoCard {
  id: number
  child_id: number
  parent_id: number
  week_start: string
  cells: AllowanceBingoCell[]
  completed_lines: number // bitmask: bits 0–7 for 8 lines
  full_card: boolean
  bonus_earned: number
  created_at: string
  updated_at: string
}

const ALLOWANCE_BINGO_LINES: [number, number, number][] = [
  [0, 1, 2], [3, 4, 5], [6, 7, 8], // rows
  [0, 3, 6], [1, 4, 7], [2, 5, 8], // cols
  [0, 4, 8], [2, 4, 6],             // diagonals
]

type Tab = 'chores' | 'earnings' | 'extras' | 'goals'

export default function MyChoresPage() {
  const { t } = useTranslation('allowance')
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
  const [pendingPhotoChoreId, setPendingPhotoChoreId] = useState<number | null>(null)
  const photoInputRef = useRef<HTMLInputElement>(null)
  const [previewFile, setPreviewFile] = useState<File | null>(null)
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  const [previewChoreId, setPreviewChoreId] = useState<number | null>(null)

  // Revoke the object URL when it changes or the component unmounts to avoid resource leaks
  useEffect(() => {
    return () => {
      if (previewUrl) URL.revokeObjectURL(previewUrl)
    }
  }, [previewUrl])

  const [teamStarting, setTeamStarting] = useState<number | null>(null)
  const [teamJoining, setTeamJoining] = useState<number | null>(null)
  const [showCelebration, setShowCelebration] = useState(false)

  const [siblings, setSiblings] = useState<SiblingInfo[]>([])

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
  const [savedInputError, setSavedInputError] = useState<Record<number, string>>({})

  const [bingoCard, setBingoCard] = useState<AllowanceBingoCard | null>(null)
  const [bingoLoading, setBingoLoading] = useState(false)
  const [bingoError, setBingoError] = useState('')
  const [showBingoCelebration, setShowBingoCelebration] = useState(false)
  const bingoFirstLoadRef = useRef(true)
  const prevBingoLinesRef = useRef(-1)
  const prevBingoFullCardRef = useRef(false)

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

  const loadBingo = useCallback(async (signal?: AbortSignal) => {
    setBingoLoading(true)
    setBingoError('')
    try {
      const res = await fetch('/api/allowance/my/bingo', { credentials: 'include', signal })
      if (!res.ok) throw new Error()
      const data: AllowanceBingoCard = await res.json()
      const prevLines = prevBingoLinesRef.current
      const prevFullCard = prevBingoFullCardRef.current
      prevBingoLinesRef.current = data.completed_lines
      prevBingoFullCardRef.current = data.full_card
      if (bingoFirstLoadRef.current) {
        bingoFirstLoadRef.current = false
        // On initial load trigger celebration only for a completed full card
        if (data.full_card) setShowBingoCelebration(true)
      } else {
        // Trigger only when new bingo lines are set (new bits in bitmask) or full_card transitions false→true
        const newLinesBits = prevLines === -1 ? 0 : (data.completed_lines & ~prevLines)
        const fullCardTransition = data.full_card && !prevFullCard
        if (newLinesBits !== 0 || fullCardTransition) setShowBingoCelebration(true)
      }
      setBingoCard(data)
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setBingoError(t('myChores.bingo.failedToLoad'))
    } finally {
      setBingoLoading(false)
    }
  }, [t])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    loadChores(controller.signal)
    return () => controller.abort()
  }, [loadChores])

  useEffect(() => {
    // Only refresh bingo when we're on the Today/chores tab
    if (tab !== 'chores') return
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    loadBingo(controller.signal)
    return () => controller.abort()
  }, [loadBingo, tab])

  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/allowance/my/siblings', { credentials: 'include', signal: controller.signal })
      .then(res => (res.ok ? res.json() : null))
      .then((data: SiblingInfo[] | null) => {
        if (Array.isArray(data)) setSiblings(data)
      })
      .catch(() => {/* siblings are optional; failures are non-fatal */})
    return () => controller.abort()
  }, [])

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

  const compressImage = (file: File): Promise<Blob> =>
    new Promise((resolve, reject) => {
      const img = new Image()
      const url = URL.createObjectURL(file)
      img.onload = () => {
        URL.revokeObjectURL(url)
        const MAX = 1024
        const scale = img.width > MAX ? MAX / img.width : 1
        const canvas = document.createElement('canvas')
        canvas.width = Math.round(img.width * scale)
        canvas.height = Math.round(img.height * scale)
        const ctx = canvas.getContext('2d')
        if (!ctx) {
          reject(new Error(t('errors.compressionFailed')))
          return
        }
        ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
        canvas.toBlob(
          (blob) => {
            if (blob) resolve(blob)
            else reject(new Error(t('errors.compressionFailed')))
          },
          'image/jpeg',
          0.85,
        )
      }
      img.onerror = () => {
        URL.revokeObjectURL(url)
        reject(new Error(t('errors.compressionFailed')))
      }
      img.src = url
    })

  const handleComplete = async (choreId: number, photoFile?: File) => {
    setCompleting(choreId)
    setActionError('')
    try {
      let res: Response
      if (photoFile) {
        const compressed = await compressImage(photoFile)
        const form = new FormData()
        form.append('photo', compressed, 'photo.jpg')
        res = await fetch(`/api/allowance/my/complete/${choreId}`, {
          method: 'POST',
          credentials: 'include',
          body: form,
        })
      } else {
        res = await fetch(`/api/allowance/my/complete/${choreId}`, {
          method: 'POST',
          credentials: 'include',
        })
      }
      if (!res.ok) throw new Error()
      if (previewUrl) URL.revokeObjectURL(previewUrl)
      setPreviewFile(null)
      setPreviewUrl(null)
      setPreviewChoreId(null)
      setPendingPhotoChoreId(null)
      // Refresh chores to get updated status
      await loadChores()
    } catch {
      setActionError(t('errors.actionFailed'))
    } finally {
      setCompleting(null)
    }
  }

  const handleRetakePhoto = () => {
    if (previewUrl) URL.revokeObjectURL(previewUrl)
    setPreviewFile(null)
    setPreviewUrl(null)
    setPreviewChoreId(null)
    photoInputRef.current?.click()
  }

  const handleTeamStart = async (choreId: number) => {
    setTeamStarting(choreId)
    setActionError('')
    try {
      const res = await fetch(`/api/allowance/my/team-start/${choreId}`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      if (!res.ok) throw new Error()
      await loadChores()
    } catch {
      setActionError(t('errors.actionFailed'))
    } finally {
      setTeamStarting(null)
    }
  }

  const handleTeamJoin = async (completionId: number) => {
    setTeamJoining(completionId)
    setActionError('')
    try {
      const res = await fetch(`/api/allowance/my/team-join/${completionId}`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error()
      const data: { status?: string } = await res.json()
      if (data.status === 'pending') {
        setShowCelebration(true)
      }
      await loadChores()
    } catch {
      setActionError(t('errors.actionFailed'))
    } finally {
      setTeamJoining(null)
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
      setSavedInputError(prev => ({ ...prev, [goalId]: t('errors.amountInvalid') }))
      return
    }
    setSavedInputError(prev => ({ ...prev, [goalId]: '' }))
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

  const formatCurrency = (currency: string) =>
    !currency || currency === 'NOK' ? t('currency') : currency

  const formatAmount = (amount: number, currency: string) => {
    const curr = formatCurrency(currency)
    return `${amount} ${curr}`
  }

  const formatWeekRange = (weekStart: string) => {
    // weekStart is "YYYY-MM-DD"; parse as local midnight to avoid UTC off-by-one issues
    const start = new Date(`${weekStart}T00:00:00`)
    const end = new Date(start)
    end.setDate(end.getDate() + 6)
    return `${formatDate(start, { month: 'short', day: 'numeric' })} – ${formatDate(end, { month: 'short', day: 'numeric' })}`
  }

  const doneChores = chores.filter(c => c.completion_status === 'approved')
  const pendingChores = chores.filter(c => c.completion_status === 'pending')
  const todoChores = chores.filter(
    c => !c.completion_status || c.completion_status === 'rejected' || c.completion_status === 'waiting_for_team',
  )

  return (
    <div className="max-w-lg mx-auto px-4 py-6 space-y-6">
      {/* Hidden file input for chore photo upload — capture="environment" requests the rear camera on supported mobile browsers */}
      <input
        ref={photoInputRef}
        type="file"
        accept="image/*"
        capture="environment"
        className="hidden"
        aria-hidden="true"
        onChange={e => {
          const file = e.target.files?.[0]
          if (file && pendingPhotoChoreId !== null) {
            const url = URL.createObjectURL(file)
            setPreviewFile(file)
            setPreviewUrl(url)
            setPreviewChoreId(pendingPhotoChoreId)
          }
          e.target.value = ''
        }}
      />
      <Confetti active={showCelebration} onDone={() => setShowCelebration(false)} />
      <Confetti active={showBingoCelebration} onDone={() => setShowBingoCelebration(false)} />
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
            aria-controls={`tabpanel-${id}`}
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
        <div id="tabpanel-chores" role="tabpanel" aria-labelledby="tab-chores" className="space-y-4">
          {choresLoading && (
            <div className="space-y-3 py-4" role="status" aria-live="polite" aria-busy="true">
              <p className="sr-only">{t('loading')}</p>
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
            </div>
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
              {todoChores.map(chore => {
                const isTeamMode = chore.completion_mode === 'team'
                const isEitherMode = chore.completion_mode === 'either'
                const isTeamCapable = isTeamMode || isEitherMode
                const teamSession = chore.active_team_session
                const alreadyJoined = teamSession?.current_child_joined ?? false
                const waitingForTeam = chore.completion_status === 'waiting_for_team'

                if (isTeamCapable) {
                  return (
                    <div
                      key={chore.id}
                      className="bg-gray-800 rounded-2xl p-4 space-y-3"
                    >
                      {/* Chore header */}
                      <div className="flex items-center gap-4">
                        <span className="text-4xl select-none">{chore.icon || '📋'}</span>
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2 flex-wrap">
                            <p className="text-white font-semibold text-lg leading-tight">{chore.name}</p>
                            <span className="inline-flex items-center gap-1 bg-purple-800/60 text-purple-300 text-xs font-semibold px-2 py-0.5 rounded-full">
                              <Users size={12} />
                              {t('myChores.team.badge')}
                            </span>
                          </div>
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
                          {chore.team_bonus_pct > 0 && (
                            <p className="text-purple-400 text-xs mt-0.5">
                              {t('myChores.team.bonus', { pct: chore.team_bonus_pct })}
                            </p>
                          )}
                        </div>
                      </div>

                      {/* Team progress indicator */}
                      {teamSession && (
                        <div className="flex items-center gap-2 text-sm">
                          <div className="flex gap-1">
                            {Array.from({ length: chore.min_team_size }).map((_, i) => {
                              const pid = teamSession.participant_ids[i]
                              const sibling = pid !== undefined
                                ? siblings.find(s => s.child_id === pid)
                                : undefined
                              const filled = i < teamSession.participant_count
                              return (
                                <div
                                  key={i}
                                  title={sibling?.nickname}
                                  className={`w-7 h-7 rounded-full flex items-center justify-center text-sm select-none ${
                                    filled
                                      ? 'bg-purple-600 text-white'
                                      : 'bg-gray-700 text-gray-500'
                                  }`}
                                >
                                  {filled
                                    ? (sibling?.avatar_emoji || sibling?.nickname?.charAt(0)?.toUpperCase() || '✓')
                                    : '·'}
                                </div>
                              )
                            })}
                          </div>
                          <span className="text-gray-400 text-xs">
                            {t('myChores.team.progress', {
                              joined: teamSession.participant_count,
                              total: chore.min_team_size,
                            })}
                          </span>
                        </div>
                      )}

                      {/* Photo preview thumbnail (shown when user has selected a photo for this chore) */}
                      {pendingPhotoChoreId === chore.id &&
                        previewChoreId === chore.id &&
                        previewFile !== null &&
                        previewUrl !== null && (
                          <img
                            src={previewUrl}
                            alt={t('myChores.photo.previewAlt', { choreName: chore.name ?? '' })}
                            className="w-full max-h-48 object-cover rounded-xl"
                          />
                        )}

                      {/* Action buttons */}
                      <div className="flex gap-2">
                        {/* Waiting state: this child already joined, waiting for more */}
                        {(alreadyJoined || waitingForTeam) && !teamSession && (
                          <div className="flex-1 flex items-center gap-2 py-2.5 px-4 bg-purple-900/30 border border-purple-700/40 rounded-xl text-purple-300 text-sm">
                            <Clock size={14} />
                            {t('myChores.team.waitingForTeam')}
                          </div>
                        )}
                        {teamSession && alreadyJoined && (
                          <div className="flex-1 flex items-center gap-2 py-2.5 px-4 bg-purple-900/30 border border-purple-700/40 rounded-xl text-purple-300 text-sm">
                            <Clock size={14} />
                            {t('myChores.team.waitingForTeam')}
                          </div>
                        )}
                        {/* Not joined yet but session exists: show Join button */}
                        {teamSession && !alreadyJoined && (
                          <>
                            {(() => {
                              const starterID = teamSession.participant_ids[0]
                              const starter = starterID !== undefined
                                ? siblings.find(s => s.child_id === starterID)
                                : undefined
                              const joinLabel = teamJoining === teamSession.completion_id
                                ? t('myChores.team.joining')
                                : starter
                                  ? t('myChores.team.joinSibling', { name: starter.nickname })
                                  : t('myChores.team.join')
                              return (
                                <button
                                  type="button"
                                  onClick={() => handleTeamJoin(teamSession.completion_id)}
                                  disabled={teamJoining === teamSession.completion_id}
                                  className="flex-1 py-2.5 px-3 bg-purple-600 hover:bg-purple-500 active:scale-95 text-white rounded-xl font-bold text-sm transition-all cursor-pointer disabled:opacity-60"
                                >
                                  {joinLabel}
                                </button>
                              )
                            })()}
                          </>
                        )}
                        {/* No session yet: show start button(s) */}
                        {!teamSession && !alreadyJoined && !waitingForTeam && (
                          <>
                            <button
                              type="button"
                              onClick={() => handleTeamStart(chore.id)}
                              disabled={teamStarting === chore.id}
                              className="flex-1 py-2.5 px-3 bg-purple-600 hover:bg-purple-500 active:scale-95 text-white rounded-xl font-bold text-sm transition-all cursor-pointer disabled:opacity-60"
                            >
                              {teamStarting === chore.id
                                ? t('myChores.team.starting')
                                : t('myChores.team.doTogether')}
                            </button>
                            {isEitherMode && (
                              pendingPhotoChoreId === chore.id ? (
                                previewFile !== null && previewUrl !== null ? (
                                  <div className="flex-1 flex gap-2">
                                    <button
                                      type="button"
                                      onClick={handleRetakePhoto}
                                      disabled={completing === chore.id}
                                      className="flex-1 py-2.5 px-3 bg-gray-700 hover:bg-gray-600 active:scale-95 text-white rounded-xl font-semibold text-sm transition-all cursor-pointer disabled:opacity-60 flex items-center justify-center gap-2"
                                    >
                                      <Camera size={18} />
                                      {t('myChores.photo.retake')}
                                    </button>
                                    <button
                                      type="button"
                                      onClick={() => void handleComplete(chore.id, previewFile)}
                                      disabled={completing === chore.id}
                                      className="flex-1 py-2.5 px-3 bg-green-600 hover:bg-green-500 active:scale-95 text-white rounded-xl font-bold text-sm transition-all cursor-pointer disabled:opacity-60 flex items-center justify-center gap-2"
                                    >
                                      <CheckCircle2 size={18} />
                                      {completing === chore.id ? t('myChores.photo.uploading') : t('myChores.photo.confirm')}
                                    </button>
                                  </div>
                                ) : (
                                  <div className="flex-1 flex gap-2">
                                    <button
                                      type="button"
                                      onClick={() => photoInputRef.current?.click()}
                                      disabled={completing === chore.id}
                                      aria-label={t('myChores.photo.take')}
                                      className="flex-1 py-4 sm:py-2.5 px-3 bg-blue-600 hover:bg-blue-500 active:scale-95 text-white rounded-xl font-bold text-sm transition-all cursor-pointer disabled:opacity-60 flex items-center justify-center gap-2"
                                    >
                                      <Camera size={20} />
                                      {t('myChores.photo.take')}
                                    </button>
                                    <button
                                      type="button"
                                      onClick={() => handleComplete(chore.id)}
                                      disabled={completing === chore.id}
                                      className="py-4 sm:py-2.5 px-3 bg-gray-700 hover:bg-gray-600 active:scale-95 text-gray-300 rounded-xl text-sm transition-all cursor-pointer disabled:opacity-60"
                                    >
                                      {completing === chore.id ? t('myChores.photo.uploading') : t('myChores.photo.skip')}
                                    </button>
                                  </div>
                                )
                              ) : (
                                <button
                                  type="button"
                                  onClick={() => setPendingPhotoChoreId(chore.id)}
                                  disabled={completing === chore.id}
                                  className="flex-1 py-2.5 px-3 bg-gray-700 hover:bg-gray-600 active:scale-95 text-white rounded-xl font-bold text-sm transition-all cursor-pointer disabled:opacity-60"
                                >
                                  {t('myChores.team.doAlone')}
                                </button>
                              )
                            )}
                          </>
                        )}
                      </div>
                    </div>
                  )
                }

                // Default solo chore — show camera UI (or preview) after tapping Done
                if (pendingPhotoChoreId === chore.id) {
                  if (previewFile !== null && previewUrl !== null) {
                    return (
                      <div key={chore.id} className="w-full bg-gray-800 rounded-2xl p-4 space-y-3">
                        <div className="flex items-center gap-4">
                          <span className="text-4xl select-none">{chore.icon || '📋'}</span>
                          <div className="flex-1 text-left min-w-0">
                            <p className="text-white font-semibold text-lg leading-tight">{chore.name}</p>
                            <p className="text-gray-400 text-sm mt-0.5">{t('myChores.photo.preview')}</p>
                          </div>
                        </div>
                        <img
                          src={previewUrl}
                          alt={t('myChores.photo.previewAlt', { choreName: chore.name ?? '' })}
                          className="w-full max-h-48 object-cover rounded-xl"
                        />
                        <div className="flex gap-2">
                          <button
                            type="button"
                            onClick={handleRetakePhoto}
                            disabled={completing === chore.id}
                            className="flex-1 py-3 bg-gray-700 hover:bg-gray-600 active:scale-95 text-white rounded-xl font-semibold text-sm transition-all cursor-pointer disabled:opacity-60 flex items-center justify-center gap-2"
                          >
                            <Camera size={18} />
                            {t('myChores.photo.retake')}
                          </button>
                          <button
                            type="button"
                            onClick={() => void handleComplete(chore.id, previewFile)}
                            disabled={completing === chore.id}
                            className="flex-1 py-3 bg-green-600 hover:bg-green-500 active:scale-95 text-white rounded-xl font-bold text-sm transition-all cursor-pointer disabled:opacity-60 flex items-center justify-center gap-2"
                          >
                            <CheckCircle2 size={18} />
                            {completing === chore.id ? t('myChores.photo.uploading') : t('myChores.photo.confirm')}
                          </button>
                        </div>
                      </div>
                    )
                  }

                  return (
                    <div
                      key={chore.id}
                      className="w-full bg-gray-800 rounded-2xl p-4 space-y-3 sm:space-y-0 sm:flex sm:items-center sm:gap-4"
                    >
                      <div className="flex items-center gap-4">
                        <span className="text-4xl select-none">{chore.icon || '📋'}</span>
                        <div className="flex-1 text-left min-w-0">
                          <p className="text-white font-semibold text-lg leading-tight">{chore.name}</p>
                          <p className="text-gray-400 text-sm mt-0.5">{t('myChores.photo.addPhoto')}</p>
                        </div>
                        {/* Desktop: compact icon button */}
                        <div className="hidden sm:flex gap-2 shrink-0">
                          <button
                            type="button"
                            onClick={() => photoInputRef.current?.click()}
                            disabled={completing === chore.id}
                            aria-label={t('myChores.photo.take')}
                            className="p-2.5 bg-blue-600 hover:bg-blue-500 active:scale-95 text-white rounded-xl transition-all cursor-pointer disabled:opacity-60"
                          >
                            <Camera size={20} />
                          </button>
                          <button
                            type="button"
                            onClick={() => handleComplete(chore.id)}
                            disabled={completing === chore.id}
                            className="py-2 px-3 bg-gray-700 hover:bg-gray-600 active:scale-95 text-gray-300 rounded-xl text-sm transition-all cursor-pointer disabled:opacity-60"
                          >
                            {completing === chore.id ? t('myChores.photo.uploading') : t('myChores.photo.skip')}
                          </button>
                        </div>
                      </div>
                      {/* Mobile: large prominent camera button */}
                      <div className="flex gap-2 sm:hidden">
                        <button
                          type="button"
                          onClick={() => photoInputRef.current?.click()}
                          disabled={completing === chore.id}
                          aria-label={t('myChores.photo.take')}
                          className="flex-1 py-4 bg-blue-600 hover:bg-blue-500 active:scale-95 text-white rounded-xl font-bold text-base transition-all cursor-pointer disabled:opacity-60 flex items-center justify-center gap-2"
                        >
                          <Camera size={24} />
                          {t('myChores.photo.take')}
                        </button>
                        <button
                          type="button"
                          onClick={() => handleComplete(chore.id)}
                          disabled={completing === chore.id}
                          className="py-4 px-4 bg-gray-700 hover:bg-gray-600 active:scale-95 text-gray-300 rounded-xl text-sm transition-all cursor-pointer disabled:opacity-60"
                        >
                          {completing === chore.id ? t('myChores.photo.uploading') : t('myChores.photo.skip')}
                        </button>
                      </div>
                    </div>
                  )
                }

                return (
                  <button
                    key={chore.id}
                    onClick={() => setPendingPhotoChoreId(chore.id)}
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
                        <div className="mt-1" role="status" aria-live="polite">
                          <Skeleton className="h-3 w-16" aria-hidden="true" />
                          <span className="sr-only">{t('loading')}</span>
                        </div>
                      ) : (
                        <p className="text-gray-500 text-xs mt-1">{t('myChores.tap')}</p>
                      )}
                    </div>
                  </button>
                )
              })}
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

          {/* Bingo card */}
          <BingoCardSection
            card={bingoCard}
            loading={bingoLoading}
            error={bingoError}
          />
        </div>
      )}

      {/* Extras tab — Extras Board */}
      {tab === 'extras' && (
        <div id="tabpanel-extras" role="tabpanel" aria-labelledby="tab-extras" className="space-y-4">
          {extrasLoading && (
            <div className="space-y-3 py-4" role="status" aria-live="polite" aria-busy="true">
              <p className="sr-only">{t('loading')}</p>
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
            </div>
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
                return (
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
                )
              })}
            </div>
          )}
        </div>
      )}

      {/* Earnings tab */}
      {tab === 'earnings' && (
        <div id="tabpanel-earnings" role="tabpanel" aria-labelledby="tab-earnings" className="space-y-4">
          {earningsLoading && (
            <div className="space-y-3 py-4" role="status" aria-live="polite" aria-busy="true">
              <p className="sr-only">{t('loading')}</p>
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
            </div>
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
                {(() => {
                  const reversedHistory = [...history].reverse()
                  return (
                    <BarChart data={reversedHistory} margin={{ top: 0, right: 0, left: -20, bottom: 0 }}>
                      <XAxis
                        dataKey="week_start"
                        tickFormatter={(v: string) =>
                          formatDate(`${v}T00:00:00Z`, { month: 'short', day: 'numeric', timeZone: 'UTC' })
                        }
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
                        {reversedHistory.map((p) => (
                          <Cell key={p.id} fill={p.paid_out ? '#4ade80' : '#facc15'} />
                        ))}
                      </Bar>
                    </BarChart>
                  )
                })()}
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
        <div id="tabpanel-goals" role="tabpanel" aria-labelledby="tab-goals" className="space-y-4">
          {goalsLoading && (
            <div className="space-y-3 py-4" role="status" aria-live="polite" aria-busy="true">
              <p className="sr-only">{t('loading')}</p>
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
            </div>
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
                  <div className="space-y-1 pt-1">
                    <div className="flex gap-2">
                      <input
                        type="number"
                        min="0"
                        step="1"
                        aria-label={t('goals.updateSaved')}
                        value={savedInput[goal.id] ?? ''}
                        onChange={e => {
                          setSavedInput(prev => ({ ...prev, [goal.id]: e.target.value }))
                          setSavedInputError(prev => ({ ...prev, [goal.id]: '' }))
                        }}
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
                    {savedInputError[goal.id] && (
                      <p className="text-red-400 text-xs">{savedInputError[goal.id]}</p>
                    )}
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

interface BingoCardSectionProps {
  card: AllowanceBingoCard | null
  loading: boolean
  error: string
}

function BingoCardSection({ card, loading, error }: BingoCardSectionProps) {
  const { t } = useTranslation('allowance')

  if (loading) {
    return (
      <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5 space-y-3 mt-4" role="status" aria-live="polite" aria-busy="true">
        <span className="sr-only">{t('myChores.bingo.loading')}</span>
        <Skeleton className="h-5 w-40" />
        <div className="grid grid-cols-3 gap-2">
          {[...Array(9)].map((_, i) => (
            <Skeleton key={i} className="h-[72px] w-full" />
          ))}
        </div>
      </div>
    )
  }

  if (error || !card) {
    if (error) {
      return (
        <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5 mt-4">
          <p className="text-red-400 text-sm">{error}</p>
        </div>
      )
    }
    return null
  }

  // Decode bitmask into set of highlighted cell indices
  const highlightedCells = new Set<number>()
  for (let lineIdx = 0; lineIdx < 8; lineIdx++) {
    if (card.completed_lines & (1 << lineIdx)) {
      const line = ALLOWANCE_BINGO_LINES[lineIdx]
      if (line) line.forEach(ci => highlightedCells.add(ci))
    }
  }

  const completedLineCount = (() => {
    let count = 0
    for (let i = 0; i < 8; i++) {
      if (card.completed_lines & (1 << i)) count++
    }
    return count
  })()

  // Format week label: "Week of Mar 31"
  const weekLabel = (() => {
    const d = new Date(`${card.week_start}T00:00:00`)
    return formatDate(d, { month: 'short', day: 'numeric' })
  })()

  return (
    <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5 mt-4">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <span className="text-xl" role="img" aria-hidden="true">🎱</span>
          <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">
            {t('myChores.bingo.title')}
          </h2>
        </div>
        <span className="text-xs text-gray-500">
          {t('myChores.bingo.weekLabel', { date: weekLabel })}
        </span>
      </div>

      {/* Full-card jackpot banner */}
      {card.full_card && (
        <div className="mb-4 p-3 rounded-lg bg-yellow-500/20 border border-yellow-500/40 text-center animate-pulse">
          <p className="text-yellow-300 font-bold text-sm">{t('myChores.bingo.jackpot')}</p>
        </div>
      )}

      {/* 3×3 grid */}
      <div className="grid grid-cols-3 gap-2">
        {card.cells.map((cell, idx) => {
          const isHighlighted = highlightedCells.has(idx)
          return (
            <div
              key={cell.challenge_key}
              className={[
                'relative rounded-lg border p-2 min-h-[72px] flex flex-col items-center justify-center text-center transition-colors',
                cell.completed
                  ? isHighlighted
                    ? 'bg-green-500/30 border-green-400/60'
                    : 'bg-green-600/20 border-green-600/40'
                  : 'bg-gray-700/40 border-gray-600/40',
              ].join(' ')}
            >
              {cell.completed && (
                <div className="absolute inset-0 flex items-center justify-center rounded-lg bg-green-500/20">
                  <span className="text-2xl" role="img" aria-hidden="true">✅</span>
                </div>
              )}
              <p
                className={`text-xs font-medium leading-tight ${
                  cell.completed ? 'text-green-300 opacity-60' : 'text-gray-300'
                }`}
              >
                {t(`myChores.bingo.challenges.${cell.challenge_key}`, { defaultValue: cell.label })}
              </p>
            </div>
          )
        })}
      </div>

      {/* Progress summary */}
      {completedLineCount > 0 && (
        <p className="mt-3 text-center text-xs text-green-400">
          {t('myChores.bingo.linesCompleted', { lines: completedLineCount })}
        </p>
      )}

      {/* Bonus earned */}
      {card.bonus_earned > 0 && (
        <p className="mt-1 text-center text-xs text-yellow-400 font-semibold">
          {t('myChores.bingo.bonusEarned', { amount: card.bonus_earned })}
        </p>
      )}
    </div>
  )
}
