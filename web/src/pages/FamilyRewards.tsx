import { useState, useEffect, useRef, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  Gift, ArrowLeft, Check, X, CheckCircle, XCircle, Clock,
  Star, Trash2, Edit2, Plus, Sparkles,
} from 'lucide-react'
import { formatDate } from '../utils/formatDate'

interface ParentReward {
  id: number
  parent_id: number
  title: string
  description: string
  star_cost: number
  icon_emoji: string
  is_active: boolean
  max_claims: number | null
  parent_note: string
  created_at: string
  updated_at: string
}

interface ClaimDetails {
  id: number
  reward_id: number
  reward_title: string
  reward_icon: string
  star_cost: number
  child_id: number
  child_nickname: string
  child_avatar: string
  status: 'pending' | 'approved' | 'denied'
  stars_spent: number
  note?: string
  resolved_at?: string | null
  created_at: string
}

interface AddEditForm {
  title: string
  description: string
  star_cost: number
  icon_emoji: string
  max_claims: string
}

const DEFAULT_REWARDS: Array<{ title: string; description: string; icon_emoji: string; star_cost: number }> = [
  { title: 'Ice Cream', description: 'A scoop of your favourite ice cream', icon_emoji: '🍦', star_cost: 5 },
  { title: 'Movie Night', description: 'Pick a movie for the family to watch', icon_emoji: '🎬', star_cost: 20 },
  { title: 'Extra Screen Time', description: '30 minutes of extra screen time', icon_emoji: '📱', star_cost: 10 },
  { title: 'Pick Dinner', description: 'Choose what the family has for dinner', icon_emoji: '🍕', star_cost: 15 },
  { title: 'Stay Up Late', description: 'Stay up 30 minutes past bedtime', icon_emoji: '🌙', star_cost: 25 },
  { title: 'Skip a Chore', description: 'Get out of one chore for the day', icon_emoji: '🧹', star_cost: 30 },
  { title: 'New Book', description: 'Choose a new book to read', icon_emoji: '📚', star_cost: 20 },
  { title: 'Special Outing', description: 'Go somewhere fun together', icon_emoji: '🎡', star_cost: 50 },
  { title: 'New Game', description: 'A new board game or video game', icon_emoji: '🎮', star_cost: 40 },
  { title: 'Sleepover', description: 'Have a friend sleep over', icon_emoji: '🏕️', star_cost: 35 },
]

const EMPTY_FORM: AddEditForm = { title: '', description: '', star_cost: 10, icon_emoji: '🎁', max_claims: '' }

function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation('common')
  if (status === 'pending') {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-yellow-500/20 text-yellow-300 font-medium border border-yellow-500/30">
        <Clock size={10} />
        {t('stars.rewards.pending')}
      </span>
    )
  }
  if (status === 'approved') {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-green-500/20 text-green-300 font-medium border border-green-500/30">
        <CheckCircle size={10} />
        {t('stars.rewards.approved')}
      </span>
    )
  }
  if (status === 'denied') {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-red-500/20 text-red-300 font-medium border border-red-500/30">
        <XCircle size={10} />
        {t('stars.rewards.denied')}
      </span>
    )
  }
  return null
}

export default function FamilyRewards() {
  const { t } = useTranslation('common')
  const [rewards, setRewards] = useState<ParentReward[]>([])
  const [pendingClaims, setPendingClaims] = useState<ClaimDetails[]>([])
  const [allClaims, setAllClaims] = useState<ClaimDetails[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [notification, setNotification] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [refresh, setRefresh] = useState(0)
  const notificationTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Edit reward state
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editForm, setEditForm] = useState<AddEditForm>(EMPTY_FORM)
  const [saving, setSaving] = useState(false)

  // Add reward state
  const [showAddForm, setShowAddForm] = useState(false)
  const [addForm, setAddForm] = useState<AddEditForm>(EMPTY_FORM)
  const [adding, setAdding] = useState(false)

  // Delete confirm
  const [deleteConfirmId, setDeleteConfirmId] = useState<number | null>(null)

  // Deny note per claim
  const [denyNoteId, setDenyNoteId] = useState<number | null>(null)
  const [denyNote, setDenyNote] = useState('')
  const [resolving, setResolving] = useState<number | null>(null)

  // Seeding defaults
  const [seeding, setSeeding] = useState(false)

  const showNotification = useCallback((message: string, type: 'success' | 'error') => {
    setNotification({ message, type })
    if (notificationTimerRef.current !== null) clearTimeout(notificationTimerRef.current)
    notificationTimerRef.current = setTimeout(() => {
      setNotification(null)
      notificationTimerRef.current = null
    }, 4000)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    const fetchAll = async () => {
      setLoading(true)
      setError(null)
      try {
        const [rewardsRes, pendingRes, allClaimsRes] = await Promise.all([
          fetch('/api/family/rewards', { credentials: 'include', signal: controller.signal }),
          fetch('/api/family/claims?status=pending', { credentials: 'include', signal: controller.signal }),
          fetch('/api/family/claims', { credentials: 'include', signal: controller.signal }),
        ])
        if (!rewardsRes.ok || !pendingRes.ok || !allClaimsRes.ok) throw new Error('fetch failed')
        const [rewardsData, pendingData, allClaimsData] = await Promise.all([
          rewardsRes.json() as Promise<{ rewards: ParentReward[] }>,
          pendingRes.json() as Promise<{ claims: ClaimDetails[] }>,
          allClaimsRes.json() as Promise<{ claims: ClaimDetails[] }>,
        ])
        setRewards(rewardsData.rewards ?? [])
        setPendingClaims(pendingData.claims ?? [])
        setAllClaims(allClaimsData.claims ?? [])
      } catch (err: unknown) {
        if (controller.signal.aborted) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(t('family.rewards.errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    fetchAll()
    return () => controller.abort()
  }, [t, refresh])

  useEffect(() => {
    return () => {
      if (notificationTimerRef.current !== null) clearTimeout(notificationTimerRef.current)
    }
  }, [])

  async function resolveClaim(claimId: number, status: 'approved' | 'denied', note: string) {
    setResolving(claimId)
    try {
      const res = await fetch(`/api/family/claims/${claimId}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status, note }),
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        showNotification((body as { error?: string }).error ?? t('family.rewards.errors.failedToResolve'), 'error')
        return
      }
      setDenyNoteId(null)
      setDenyNote('')
      showNotification(
        status === 'approved' ? t('family.rewards.success.approved') : t('family.rewards.success.denied'),
        'success'
      )
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.rewards.errors.failedToResolve'), 'error')
    } finally {
      setResolving(null)
    }
  }

  async function deleteReward(id: number) {
    try {
      const res = await fetch(`/api/family/rewards/${id}`, { method: 'DELETE', credentials: 'include' })
      if (!res.ok) throw new Error('failed')
      setDeleteConfirmId(null)
      showNotification(t('family.rewards.success.deleted'), 'success')
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.rewards.errors.failedToDelete'), 'error')
    }
  }

  function startEdit(reward: ParentReward) {
    setEditingId(reward.id)
    setEditForm({
      title: reward.title,
      description: reward.description,
      star_cost: reward.star_cost,
      icon_emoji: reward.icon_emoji,
      max_claims: reward.max_claims !== null ? String(reward.max_claims) : '',
    })
  }

  function buildRewardPayload(form: AddEditForm) {
    const maxClaimsParsed = form.max_claims.trim() !== '' ? parseInt(form.max_claims, 10) : null
    return {
      title: form.title.trim(),
      description: form.description,
      star_cost: form.star_cost,
      icon_emoji: form.icon_emoji || '🎁',
      max_claims: maxClaimsParsed,
      is_active: true,
    }
  }

  async function saveEdit(id: number) {
    setSaving(true)
    try {
      const res = await fetch(`/api/family/rewards/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(buildRewardPayload(editForm)),
      })
      if (!res.ok) throw new Error('failed')
      setEditingId(null)
      showNotification(t('family.rewards.success.updated'), 'success')
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.rewards.errors.failedToUpdate'), 'error')
    } finally {
      setSaving(false)
    }
  }

  async function addReward() {
    if (!addForm.title.trim()) return
    setAdding(true)
    try {
      const res = await fetch('/api/family/rewards', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(buildRewardPayload(addForm)),
      })
      if (!res.ok) throw new Error('failed')
      setAddForm(EMPTY_FORM)
      setShowAddForm(false)
      showNotification(t('family.rewards.success.created'), 'success')
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.rewards.errors.failedToCreate'), 'error')
    } finally {
      setAdding(false)
    }
  }

  async function seedDefaults() {
    setSeeding(true)
    try {
      await Promise.all(
        DEFAULT_REWARDS.map(r =>
          fetch('/api/family/rewards', {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ...r, is_active: true }),
          })
        )
      )
      showNotification(t('family.rewards.success.seeded'), 'success')
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.rewards.errors.failedToSeed'), 'error')
    } finally {
      setSeeding(false)
    }
  }

  const historyItems = [...allClaims]
    .filter(c => c.status !== 'pending')
    .sort((a, b) => b.created_at.localeCompare(a.created_at))

  if (loading) {
    return (
      <div className="p-6 max-w-3xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Gift size={24} className="text-blue-400" />
          <h1 className="text-2xl font-semibold text-white">{t('family.rewards.title')}</h1>
        </div>
        <div className="space-y-4">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-24 rounded-xl bg-gray-800 animate-pulse" />
          ))}
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6 max-w-3xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Gift size={24} className="text-blue-400" />
          <h1 className="text-2xl font-semibold text-white">{t('family.rewards.title')}</h1>
        </div>
        <div className="text-red-400">{error}</div>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-3xl mx-auto space-y-8">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Link
          to="/family"
          className="text-gray-400 hover:text-white transition-colors"
          aria-label={t('actions.back')}
        >
          <ArrowLeft size={20} />
        </Link>
        <Gift size={24} className="text-blue-400" />
        <h1 className="text-2xl font-semibold text-white">{t('family.rewards.title')}</h1>
      </div>

      {/* Notification */}
      {notification && (
        <div
          className={`rounded-xl px-4 py-3 text-sm text-center border ${
            notification.type === 'success'
              ? 'bg-green-500/20 border-green-400/30 text-green-300'
              : 'bg-red-500/20 border-red-400/30 text-red-300'
          }`}
        >
          {notification.message}
        </div>
      )}

      {/* ── Section 1: Pending Claims ── */}
      <section>
        <h2 className="text-lg font-semibold text-white mb-3 flex items-center gap-2">
          {t('family.rewards.pendingClaims')}
          {pendingClaims.length > 0 && (
            <span className="text-sm px-2 py-0.5 rounded-full bg-red-500/20 text-red-300 border border-red-500/30 font-normal">
              {pendingClaims.length}
            </span>
          )}
        </h2>

        {pendingClaims.length === 0 ? (
          <div className="rounded-xl bg-gray-800/50 border border-gray-700 p-6 text-center text-gray-400 text-sm">
            {t('family.rewards.noPendingClaims')}
          </div>
        ) : (
          <div className="space-y-3">
            {pendingClaims.map(claim => (
              <div
                key={claim.id}
                className="rounded-xl bg-gray-800/50 border border-yellow-500/30 p-4"
              >
                <div className="flex items-center gap-3 mb-3">
                  <span className="text-2xl flex-shrink-0" role="img" aria-hidden="true">
                    {claim.child_avatar || '⭐'}
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="text-white font-medium">
                      {claim.child_nickname || `User #${claim.child_id}`}
                    </p>
                    <div className="flex items-center gap-1.5 text-gray-400 text-sm">
                      <span role="img" aria-hidden="true">{claim.reward_icon}</span>
                      <span className="truncate">{claim.reward_title}</span>
                    </div>
                  </div>
                  <div className="flex flex-col items-end gap-1 shrink-0">
                    <div className="flex items-center gap-1 text-yellow-400 font-bold text-sm">
                      <Star size={13} className="fill-yellow-400" />
                      {claim.stars_spent}
                    </div>
                    <span className="text-gray-500 text-xs">
                      {formatDate(claim.created_at, { dateStyle: 'medium' })}
                    </span>
                  </div>
                </div>

                {denyNoteId === claim.id ? (
                  <div className="space-y-2">
                    <input
                      value={denyNote}
                      onChange={e => setDenyNote(e.target.value)}
                      placeholder={t('family.rewards.denyNote')}
                      className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
                      aria-label={t('family.rewards.denyNote')}
                    />
                    <div className="flex gap-2">
                      <button
                        onClick={() => resolveClaim(claim.id, 'denied', denyNote)}
                        disabled={resolving === claim.id}
                        className="flex items-center gap-1.5 px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded-lg transition-colors disabled:opacity-50 cursor-pointer"
                      >
                        <XCircle size={14} />
                        {t('family.rewards.denySubmit')}
                      </button>
                      <button
                        onClick={() => { setDenyNoteId(null); setDenyNote('') }}
                        className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
                      >
                        {t('actions.cancel')}
                      </button>
                    </div>
                  </div>
                ) : (
                  <div className="flex gap-2">
                    <button
                      onClick={() => resolveClaim(claim.id, 'approved', '')}
                      disabled={resolving === claim.id}
                      className="flex items-center gap-1.5 px-3 py-1.5 bg-green-600 hover:bg-green-700 text-white text-sm rounded-lg transition-colors disabled:opacity-50 cursor-pointer"
                    >
                      <CheckCircle size={14} />
                      {t('family.rewards.approve')}
                    </button>
                    <button
                      onClick={() => { setDenyNoteId(claim.id); setDenyNote('') }}
                      className="flex items-center gap-1.5 px-3 py-1.5 bg-red-900/50 hover:bg-red-900/80 text-red-300 text-sm rounded-lg border border-red-700 transition-colors cursor-pointer"
                    >
                      <XCircle size={14} />
                      {t('family.rewards.deny')}
                    </button>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* ── Section 2: Reward Catalog ── */}
      <section>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-lg font-semibold text-white">{t('family.rewards.catalog')}</h2>
          <button
            onClick={() => { setShowAddForm(v => !v); setAddForm(EMPTY_FORM) }}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm rounded-lg transition-colors cursor-pointer"
          >
            <Plus size={14} />
            {t('family.rewards.addReward')}
          </button>
        </div>

        {/* ── Section 3: Add Reward Form ── */}
        {showAddForm && (
          <div className="mb-4 rounded-xl bg-gray-800/70 border border-gray-700 p-4 space-y-3">
            <h3 className="text-white font-medium text-sm">{t('family.rewards.addReward')}</h3>
            <RewardFormFields form={addForm} onChange={setAddForm} idPrefix="add" />
            <div className="flex gap-2">
              <button
                onClick={addReward}
                disabled={adding || !addForm.title.trim()}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
              >
                {adding ? '...' : t('family.rewards.form.create')}
              </button>
              <button
                onClick={() => setShowAddForm(false)}
                className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
              >
                {t('actions.cancel')}
              </button>
            </div>
          </div>
        )}

        {/* ── Section 4: Seed Defaults (visible only when no rewards exist) ── */}
        {rewards.length === 0 && !showAddForm && (
          <div className="mb-4 rounded-xl bg-blue-500/10 border border-blue-500/30 p-4 flex items-center justify-between gap-4">
            <div>
              <p className="text-white font-medium text-sm">{t('family.rewards.seedTitle')}</p>
              <p className="text-gray-400 text-xs mt-0.5">{t('family.rewards.seedHint')}</p>
            </div>
            <button
              onClick={seedDefaults}
              disabled={seeding}
              className="flex items-center gap-1.5 px-3 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer shrink-0"
            >
              <Sparkles size={14} />
              {seeding ? '...' : t('family.rewards.seedDefaults')}
            </button>
          </div>
        )}

        {rewards.length === 0 ? (
          <div className="rounded-xl bg-gray-800/50 border border-gray-700 p-8 text-center space-y-2">
            <p className="text-3xl" role="img" aria-hidden="true">🎁</p>
            <p className="text-gray-300 font-medium">{t('family.rewards.noCatalog')}</p>
          </div>
        ) : (
          <div className="space-y-2">
            {rewards.map(reward => (
              <div key={reward.id} className="rounded-xl bg-gray-800/50 border border-gray-700 p-4">
                {editingId === reward.id ? (
                  <div className="space-y-3">
                    <RewardFormFields form={editForm} onChange={setEditForm} idPrefix={`edit-${reward.id}`} />
                    <div className="flex gap-2">
                      <button
                        onClick={() => saveEdit(reward.id)}
                        disabled={saving || !editForm.title.trim()}
                        className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
                      >
                        <Check size={14} />
                        {t('family.rewards.form.update')}
                      </button>
                      <button
                        onClick={() => setEditingId(null)}
                        className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
                      >
                        <X size={14} />
                        {t('actions.cancel')}
                      </button>
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center gap-3">
                    <span className="text-2xl flex-shrink-0" role="img" aria-hidden="true">
                      {reward.icon_emoji}
                    </span>
                    <div className="flex-1 min-w-0">
                      <p className="text-white font-medium truncate">{reward.title}</p>
                      {reward.description && (
                        <p className="text-gray-400 text-xs truncate">{reward.description}</p>
                      )}
                    </div>
                    <div className="flex items-center gap-1 text-yellow-400 font-bold text-sm shrink-0">
                      <Star size={12} className="fill-yellow-400" />
                      {reward.star_cost}
                    </div>
                    {reward.max_claims !== null && (
                      <span className="text-xs text-gray-500 shrink-0">
                        {t('family.rewards.maxLabel', { n: reward.max_claims })}
                      </span>
                    )}
                    <div className="flex items-center gap-1 shrink-0">
                      <button
                        onClick={() => startEdit(reward)}
                        className="p-1.5 text-gray-400 hover:text-white transition-colors cursor-pointer"
                        title={t('family.rewards.edit')}
                        aria-label={t('family.rewards.edit')}
                      >
                        <Edit2 size={14} aria-hidden="true" />
                      </button>
                      <button
                        onClick={() => setDeleteConfirmId(reward.id)}
                        className="p-1.5 text-gray-400 hover:text-red-400 transition-colors cursor-pointer"
                        title={t('family.rewards.delete')}
                        aria-label={t('family.rewards.delete')}
                      >
                        <Trash2 size={14} aria-hidden="true" />
                      </button>
                    </div>
                  </div>
                )}

                {deleteConfirmId === reward.id && (
                  <div className="mt-3 pt-3 border-t border-gray-700">
                    <p className="text-sm text-red-400 mb-2">{t('family.rewards.deleteConfirm')}</p>
                    <div className="flex gap-2">
                      <button
                        onClick={() => deleteReward(reward.id)}
                        className="px-3 py-1 bg-red-700 hover:bg-red-600 text-white text-xs rounded transition-colors cursor-pointer"
                      >
                        {t('actions.confirm')}
                      </button>
                      <button
                        onClick={() => setDeleteConfirmId(null)}
                        className="px-3 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs rounded transition-colors cursor-pointer"
                      >
                        {t('actions.cancel')}
                      </button>
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* ── Section 5: Claim History ── */}
      <section>
        <h2 className="text-lg font-semibold text-white mb-3">{t('family.rewards.claimHistory')}</h2>
        {historyItems.length === 0 ? (
          <div className="rounded-xl bg-gray-800/50 border border-gray-700 p-6 text-center text-gray-400 text-sm">
            {t('family.rewards.noHistory')}
          </div>
        ) : (
          <div className="space-y-2">
            {historyItems.map(claim => (
              <div
                key={claim.id}
                className="flex items-center gap-3 rounded-xl bg-gray-800/50 border border-gray-700 px-4 py-3"
              >
                <span className="text-xl shrink-0" role="img" aria-hidden="true">
                  {claim.child_avatar || '⭐'}
                </span>
                <div className="flex-1 min-w-0">
                  <p className="text-white text-sm font-medium truncate">
                    {claim.child_nickname || `User #${claim.child_id}`}
                  </p>
                  <div className="flex items-center gap-1 text-gray-400 text-xs">
                    <span role="img" aria-hidden="true">{claim.reward_icon}</span>
                    <span className="truncate">{claim.reward_title}</span>
                  </div>
                </div>
                <div className="flex flex-col items-end gap-1 shrink-0">
                  <div className="flex items-center gap-1 text-yellow-400 text-sm font-bold">
                    <Star size={12} className="fill-yellow-400" />
                    {claim.stars_spent}
                  </div>
                  <StatusBadge status={claim.status} />
                </div>
                <div className="text-gray-500 text-xs shrink-0">
                  {formatDate(claim.created_at, { dateStyle: 'medium' })}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}

interface RewardFormFieldsProps {
  form: AddEditForm
  onChange: (updater: (prev: AddEditForm) => AddEditForm) => void
  idPrefix: string
}

function RewardFormFields({ form, onChange, idPrefix }: RewardFormFieldsProps) {
  const { t } = useTranslation('common')
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
      <div>
        <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-emoji`}>
          {t('family.rewards.form.emoji')}
        </label>
        <input
          id={`${idPrefix}-emoji`}
          value={form.icon_emoji}
          onChange={e => onChange(f => ({ ...f, icon_emoji: e.target.value }))}
          className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-xl text-center"
          maxLength={4}
        />
      </div>
      <div>
        <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-title`}>
          {t('family.rewards.form.title')}
        </label>
        <input
          id={`${idPrefix}-title`}
          value={form.title}
          onChange={e => onChange(f => ({ ...f, title: e.target.value }))}
          placeholder={t('family.rewards.form.titlePlaceholder')}
          className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
        />
      </div>
      <div className="sm:col-span-2">
        <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-desc`}>
          {t('family.rewards.form.description')}
        </label>
        <input
          id={`${idPrefix}-desc`}
          value={form.description}
          onChange={e => onChange(f => ({ ...f, description: e.target.value }))}
          className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
        />
      </div>
      <div>
        <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-cost`}>
          {t('family.rewards.form.starCost')}
        </label>
        <input
          id={`${idPrefix}-cost`}
          type="number"
          min={0}
          value={form.star_cost}
          onChange={e => onChange(f => ({ ...f, star_cost: Math.max(0, parseInt(e.target.value, 10) || 0) }))}
          className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
        />
      </div>
      <div>
        <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-max`}>
          {t('family.rewards.form.maxClaims')}
        </label>
        <input
          id={`${idPrefix}-max`}
          type="number"
          min={1}
          value={form.max_claims}
          onChange={e => onChange(f => ({ ...f, max_claims: e.target.value }))}
          placeholder={t('family.rewards.form.unlimited')}
          className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
        />
      </div>
    </div>
  )
}
