import React, { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Users, Copy, Plus, Trash2, Edit2, Check, X, Flame, Star, TrendingUp, TrendingDown, Minus, ExternalLink, Gift, Sparkles } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth'
import { formatNumber } from '../utils/formatDate'
import { xpProgressPercent } from '../utils/stars'

interface FamilyLink {
  id: number
  parent_id: number
  child_id: number
  nickname: string
  avatar_emoji: string
  created_at: string
}

interface InviteCode {
  id: number
  code: string
  parent_id: number
  used: boolean
  expires_at: string
  created_at: string
}

interface FamilyStatus {
  is_parent: boolean
  is_child: boolean
}

interface ChildStats {
  current_balance: number
  total_earned: number
  total_spent: number
  level: number
  xp: number
  title: string
  current_streak: number
  longest_streak: number
  this_week_stars: number
  this_week_starred_workouts: number
  last_week_stars: number
  last_week_starred_workouts: number
}


export default function Family() {
  const { t } = useTranslation('common')
  const { user, refreshFamilyStatus } = useAuth()
  const [status, setStatus] = useState<FamilyStatus | null>(null)
  const [children, setChildren] = useState<FamilyLink[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [invite, setInvite] = useState<InviteCode | null>(null)
  const [generating, setGenerating] = useState(false)
  const [copied, setCopied] = useState(false)
  const [inviteInput, setInviteInput] = useState('')
  const [accepting, setAccepting] = useState(false)
  const [acceptError, setAcceptError] = useState('')
  const [removeConfirmId, setRemoveConfirmId] = useState<number | null>(null)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editNickname, setEditNickname] = useState('')
  const [editEmoji, setEditEmoji] = useState('')
  const [saving, setSaving] = useState(false)
  const [statsMap, setStatsMap] = useState<Record<number, ChildStats>>({})
  const [statsLoading, setStatsLoading] = useState(false)
  const statsAbortRef = useRef<AbortController | null>(null)
  const [awardingForChild, setAwardingForChild] = useState<FamilyLink | null>(null)
  const [awardAmount, setAwardAmount] = useState('')
  const [awardReason, setAwardReason] = useState('')
  const [awardDescription, setAwardDescription] = useState('')
  const [awarding, setAwarding] = useState(false)
  const [awardSuccess, setAwardSuccess] = useState(false)

  useEffect(() => {
    loadData()
  }, [])

  async function loadData() {
    try {
      setLoading(true)
      setError('')
      const [statusRes, childrenRes] = await Promise.all([
        fetch('/api/family/status', { credentials: 'include' }),
        fetch('/api/family/children', { credentials: 'include' }),
      ])
      if (!statusRes.ok || !childrenRes.ok) {
        throw new Error('failed')
      }
      const statusData = await statusRes.json()
      const childrenData = await childrenRes.json()
      setStatus(statusData)
      const kids: FamilyLink[] = childrenData.children ?? []
      setChildren(kids)
      if (kids.length > 0) {
        loadStats(kids)
      }
    } catch {
      setError(t('family.errors.failedToLoad'))
    } finally {
      setLoading(false)
    }
  }

  async function loadStats(kids: FamilyLink[]) {
    // Cancel any in-flight stats request before starting a new one.
    statsAbortRef.current?.abort()
    const controller = new AbortController()
    statsAbortRef.current = controller
    const { signal } = controller

    setStatsLoading(true)
    try {
      const results = await Promise.all(
        kids.map(async (child) => {
          try {
            const res = await fetch(`/api/family/children/${child.child_id}/stats`, {
              credentials: 'include',
              signal,
            })
            if (!res.ok) return null
            const data: ChildStats = await res.json()
            return { id: child.child_id, stats: data }
          } catch {
            return null
          }
        })
      )
      // Don't update state if this request was superseded or the component unmounted.
      if (signal.aborted) return
      const map: Record<number, ChildStats> = {}
      for (const r of results) {
        if (r) map[r.id] = r.stats
      }
      setStatsMap(map)
    } finally {
      if (!signal.aborted) setStatsLoading(false)
    }
  }

  async function generateInvite() {
    try {
      setGenerating(true)
      const res = await fetch('/api/family/invite', {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('failed')
      const data = await res.json()
      setInvite(data.invite)
    } catch {
      setError(t('family.errors.failedToGenerate'))
    } finally {
      setGenerating(false)
    }
  }

  async function copyCode() {
    if (!invite) return
    try {
      await navigator.clipboard.writeText(invite.code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // ignore clipboard errors
    }
  }

  async function acceptInvite() {
    if (!inviteInput.trim()) return
    try {
      setAccepting(true)
      setAcceptError('')
      const res = await fetch('/api/family/invite/accept', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: inviteInput.trim().toUpperCase() }),
      })
      const data = await res.json()
      if (!res.ok) {
        let translationKey: 'family.errors.invalidCode' | 'family.errors.usedCode' | 'family.errors.alreadyLinked' | 'family.errors.cannotLinkParent' | 'family.errors.expiredCode' | 'family.errors.failedToAccept'
        switch (res.status) {
          case 400:
            translationKey = 'family.errors.invalidCode'
            break
          case 404:
            translationKey = 'family.errors.invalidCode'
            break
          case 409: {
            // 409 covers several distinct cases; inspect the error text to
            // choose a more accurate, specific message where possible.
            const errorMessage = (data.error ?? '') as string
            if (errorMessage.includes('already been used')) {
              translationKey = 'family.errors.usedCode'
            } else if (errorMessage.includes('already linked')) {
              translationKey = 'family.errors.alreadyLinked'
            } else if (errorMessage.includes('linked children')) {
              translationKey = 'family.errors.cannotLinkParent'
            } else {
              translationKey = 'family.errors.failedToAccept'
            }
            break
          }
          case 410:
            translationKey = 'family.errors.expiredCode'
            break
          default:
            translationKey = 'family.errors.failedToAccept'
            break
        }
        setAcceptError(t(translationKey))
        return
      }
      setInviteInput('')
      await refreshFamilyStatus()
      await loadData()
    } catch {
      setAcceptError(t('family.errors.failedToAccept'))
    } finally {
      setAccepting(false)
    }
  }

  async function removeChild(childId: number) {
    try {
      const res = await fetch(`/api/family/children/${childId}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('failed')
      setRemoveConfirmId(null)
      await refreshFamilyStatus()
      await loadData()
    } catch {
      setError(t('family.errors.failedToRemove'))
    }
  }

  function startEdit(child: FamilyLink) {
    setEditingId(child.child_id)
    setEditNickname(child.nickname)
    setEditEmoji(child.avatar_emoji)
  }

  async function saveEdit(childId: number) {
    try {
      setSaving(true)
      const res = await fetch(`/api/family/children/${childId}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ nickname: editNickname, avatar_emoji: editEmoji }),
      })
      if (!res.ok) throw new Error('failed')
      setEditingId(null)
      await loadData()
    } catch {
      setError(t('family.errors.failedToUpdate'))
    } finally {
      setSaving(false)
    }
  }

  function openAwardModal(child: FamilyLink) {
    setAwardingForChild(child)
    setAwardAmount('')
    setAwardReason('')
    setAwardDescription('')
    setAwardSuccess(false)
  }

  function closeAwardModal() {
    setAwardingForChild(null)
    setAwardSuccess(false)
  }

  async function submitAward() {
    if (!awardingForChild) return
    const amount = parseInt(awardAmount, 10)
    if (isNaN(amount) || amount === 0) return
    if (!awardReason.trim()) return
    try {
      setAwarding(true)
      const res = await fetch('/api/admin/stars/award', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          user_id: awardingForChild.child_id,
          amount,
          reason: awardReason.trim(),
          description: awardDescription.trim(),
        }),
      })
      if (!res.ok) throw new Error('failed')
      setAwardSuccess(true)
      // Refresh stats to show updated balance.
      loadStats(children)
    } catch {
      setError(t('family.errors.failedToAward'))
    } finally {
      setAwarding(false)
    }
  }

  // Compute weekly summary totals across all children.
  const weeklySummary = children.reduce(
    (acc, child) => {
      const s = statsMap[child.child_id]
      if (!s) return acc
      return {
        thisWeekWorkouts: acc.thisWeekWorkouts + s.this_week_starred_workouts,
        thisWeekStars: acc.thisWeekStars + s.this_week_stars,
        lastWeekWorkouts: acc.lastWeekWorkouts + s.last_week_starred_workouts,
        lastWeekStars: acc.lastWeekStars + s.last_week_stars,
      }
    },
    { thisWeekWorkouts: 0, thisWeekStars: 0, lastWeekWorkouts: 0, lastWeekStars: 0 }
  )

  if (loading) {
    return (
      <div className="p-6 text-gray-400">{t('status.loading')}...</div>
    )
  }

  return (
    <div className="p-6 max-w-2xl mx-auto">
      <div className="flex items-center gap-3 mb-6">
        <Users size={24} className="text-blue-400" />
        <h1 className="text-2xl font-semibold text-white">{t('family.title')}</h1>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/40 border border-red-700 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* Parent view: manage children (shown for any non-child user, including new users) */}
      {!status?.is_child && <section className="mb-8">

        {/* Quick actions */}
        <div className="flex gap-3 mb-6">
          <Link
            to="/family/rewards"
            className="flex items-center gap-2 px-4 py-2.5 bg-gray-800 hover:bg-gray-700 border border-gray-700 rounded-xl text-sm text-gray-300 hover:text-white transition-colors"
          >
            <Gift size={16} />
            {t('family.manageRewards')}
          </Link>
        </div>

        {/* Children Overview Cards */}
        {children.length > 0 && (
          <div className="mb-6">
            <h2 className="text-lg font-medium text-white mb-4">{t('family.overview')}</h2>
            <div className="space-y-4">
              {children.map(child => {
                const stats = statsMap[child.child_id]
                const progressPct = stats ? xpProgressPercent(stats.level, stats.xp) : 0
                return (
                  <div
                    key={`overview-${child.child_id}`}
                    className="rounded-xl bg-gradient-to-br from-blue-500/10 to-indigo-500/10 border border-blue-500/20 p-4"
                  >
                    <div className="flex items-start gap-4">
                      {/* Avatar */}
                      <div className="flex-shrink-0 w-12 h-12 flex items-center justify-center rounded-full bg-gray-700/60 text-2xl">
                        <span aria-hidden="true">{child.avatar_emoji || '⭐'}</span>
                      </div>

                      {/* Main content */}
                      <div className="flex-1 min-w-0">
                        {/* Name + level */}
                        <div className="flex items-center justify-between gap-2 flex-wrap">
                          <p className="text-white font-semibold truncate">
                            {child.nickname || t('family.unknownChild', { id: child.child_id })}
                          </p>
                          {stats && (
                            <span className="flex-shrink-0 text-xs bg-yellow-500/20 text-yellow-300 border border-yellow-500/30 rounded-full px-2 py-0.5">
                              {t('stars.level', { level: stats.level })} · {stats.title}
                            </span>
                          )}
                        </div>

                        {/* XP progress bar */}
                        {stats && (
                          <div className="mt-2">
                            <div
                              className="w-full h-1.5 bg-gray-700 rounded-full overflow-hidden"
                              role="progressbar"
                              aria-valuenow={progressPct}
                              aria-valuemin={0}
                              aria-valuemax={100}
                              aria-label={t('family.xpProgress')}
                            >
                              <div
                                className="h-full bg-gradient-to-r from-yellow-400 to-orange-400 rounded-full transition-all duration-300"
                                style={{ width: `${progressPct}%` }}
                              />
                            </div>
                            <p className="text-xs text-gray-500 mt-0.5">
                              {t('family.xpValue', { xp: formatNumber(stats.xp) })}
                            </p>
                          </div>
                        )}

                        {/* Stats row */}
                        {statsLoading && !stats ? (
                          <p className="text-xs text-gray-500 mt-2">{t('status.loading')}...</p>
                        ) : stats ? (
                          <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
                            <div className="flex items-center gap-1.5 bg-gray-800/60 rounded-lg px-2.5 py-1.5">
                              <Star size={14} className="text-yellow-400 flex-shrink-0" />
                              <span className="text-white text-sm font-medium">{formatNumber(stats.current_balance)}</span>
                            </div>
                            <div className="flex items-center gap-1.5 bg-gray-800/60 rounded-lg px-2.5 py-1.5">
                              <Flame size={14} className={stats.current_streak > 0 ? 'text-orange-400 flex-shrink-0' : 'text-gray-500 flex-shrink-0'} />
                              <span className="text-white text-sm font-medium">{formatNumber(stats.current_streak)}</span>
                            </div>
                            <div className="flex items-center gap-1.5 bg-gray-800/60 rounded-lg px-2.5 py-1.5">
                              <Users size={14} className="text-blue-400 flex-shrink-0" />
                              <span className="text-white text-sm font-medium">{formatNumber(stats.this_week_starred_workouts)}</span>
                              <span className="text-gray-400 text-xs hidden sm:inline">{t('family.workoutsThisWeek')}</span>
                            </div>
                            <div className="flex items-center gap-1.5 bg-gray-800/60 rounded-lg px-2.5 py-1.5">
                              <Star size={14} className="text-yellow-400 flex-shrink-0" />
                              <span className="text-white text-sm font-medium">{formatNumber(stats.this_week_stars)}</span>
                              <span className="text-gray-400 text-xs hidden sm:inline">{t('family.starsThisWeek')}</span>
                            </div>
                          </div>
                        ) : null}

                        {/* Footer: View Details + management actions */}
                        <div className="mt-3 flex items-center justify-between flex-wrap gap-2">
                          <Link
                            to={`/family/children/${child.child_id}`}
                            className="flex items-center gap-1 text-blue-400 hover:text-blue-300 text-sm transition-colors"
                          >
                            <ExternalLink size={12} />
                            {t('family.viewDetails')}
                          </Link>
                          <div className="flex items-center gap-1">
                            {user?.is_admin && (
                              <button
                                onClick={() => openAwardModal(child)}
                                className="p-1.5 text-gray-400 hover:text-yellow-400 transition-colors cursor-pointer"
                                title={t('family.awardStars.button')}
                                aria-label={t('family.awardStars.button')}
                              >
                                <Sparkles size={14} aria-hidden="true" />
                              </button>
                            )}
                            <button
                              onClick={() => startEdit(child)}
                              className="p-1.5 text-gray-400 hover:text-white transition-colors cursor-pointer"
                              title={t('family.editChild')}
                              aria-label={t('family.editChild')}
                            >
                              <Edit2 size={14} aria-hidden="true" />
                            </button>
                            <button
                              onClick={() => setRemoveConfirmId(child.child_id)}
                              className="p-1.5 text-gray-400 hover:text-red-400 transition-colors cursor-pointer"
                              title={t('family.removeChild')}
                              aria-label={t('family.removeChild')}
                            >
                              <Trash2 size={14} aria-hidden="true" />
                            </button>
                          </div>
                        </div>

                        {/* Edit form (inline) */}
                        {editingId === child.child_id && (
                          <div className="mt-3 flex items-center gap-2 pt-3 border-t border-gray-700">
                            <input
                              value={editEmoji}
                              onChange={e => setEditEmoji(e.target.value)}
                              className="w-12 text-center bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-xl"
                              maxLength={4}
                              aria-label={t('family.avatarEmoji')}
                            />
                            <input
                              value={editNickname}
                              onChange={e => setEditNickname(e.target.value)}
                              placeholder={t('family.nickname')}
                              className="flex-1 bg-gray-700 border border-gray-600 rounded px-3 py-1.5 text-white text-sm"
                              aria-label={t('family.nickname')}
                            />
                            <button
                              onClick={() => saveEdit(child.child_id)}
                              disabled={saving}
                              className="p-1.5 text-green-400 hover:text-green-300 transition-colors cursor-pointer"
                              title={t('family.saveChild')}
                              aria-label={t('family.saveChild')}
                            >
                              <Check size={16} aria-hidden="true" />
                            </button>
                            <button
                              onClick={() => setEditingId(null)}
                              className="p-1.5 text-gray-400 hover:text-gray-300 transition-colors cursor-pointer"
                              title={t('actions.cancel')}
                              aria-label={t('actions.cancel')}
                            >
                              <X size={16} aria-hidden="true" />
                            </button>
                          </div>
                        )}

                        {/* Remove confirmation */}
                        {removeConfirmId === child.child_id && (
                          <div className="mt-3 pt-3 border-t border-gray-700">
                            <p className="text-sm text-red-400 mb-2">{t('family.removeConfirm')}</p>
                            <p className="text-xs text-gray-500 mb-2">{t('family.removeConfirmHint')}</p>
                            <div className="flex gap-2">
                              <button
                                onClick={() => removeChild(child.child_id)}
                                className="px-3 py-1 bg-red-700 hover:bg-red-600 text-white text-xs rounded transition-colors cursor-pointer"
                              >
                                {t('actions.confirm')}
                              </button>
                              <button
                                onClick={() => setRemoveConfirmId(null)}
                                className="px-3 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs rounded transition-colors cursor-pointer"
                              >
                                {t('actions.cancel')}
                              </button>
                            </div>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* Weekly Summary */}
        {children.length > 0 && Object.keys(statsMap).length > 0 && (
          <div className="mb-6">
            <h2 className="text-lg font-medium text-white mb-4">{t('family.weeklySummary')}</h2>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <WeeklyStat
                label={t('stars.weeklyWorkouts')}
                current={weeklySummary.thisWeekWorkouts}
                previous={weeklySummary.lastWeekWorkouts}
              />
              <WeeklyStat
                label={t('stars.weeklyStars')}
                current={weeklySummary.thisWeekStars}
                previous={weeklySummary.lastWeekStars}
                icon={<Star size={16} className="text-yellow-400" />}
              />
            </div>
          </div>
        )}

        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-medium text-white">{t('family.children')}</h2>
          <button
            onClick={generateInvite}
            disabled={generating}
            className="flex items-center gap-2 px-3 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
          >
            <Plus size={16} />
            {t('family.addChild')}
          </button>
        </div>

        {/* Invite code display */}
        {invite && (
          <div className="mb-4 p-4 bg-gray-800 border border-gray-700 rounded-lg">
            <p className="text-sm text-gray-400 mb-2">{t('family.inviteCode')}</p>
            <div className="flex items-center gap-3">
              <span className="text-2xl font-mono font-bold text-white tracking-widest">{invite.code}</span>
              <button
                onClick={copyCode}
                className="flex items-center gap-1.5 px-2.5 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
                title={t('family.copyInvite')}
              >
                <Copy size={14} />
                {copied ? t('family.inviteCopied') : t('family.copyInvite')}
              </button>
            </div>
            <p className="text-xs text-gray-500 mt-2">{t('family.inviteExpiry')}</p>
          </div>
        )}

        {children.length === 0 && (
          <div className="p-6 text-center bg-gray-800/50 rounded-lg border border-gray-700">
            <p className="text-gray-400 font-medium">{t('family.noChildren')}</p>
            <p className="text-gray-500 text-sm mt-1">{t('family.noChildrenHint')}</p>
          </div>
        )}
      </section>}

      {/* Child view: join a family (hidden for parents and existing children) */}
      {(!status?.is_child && !status?.is_parent) && (
        <section>
          <h2 className="text-lg font-medium text-white mb-4">{t('family.joinFamily')}</h2>
          <div className="p-4 bg-gray-800 border border-gray-700 rounded-lg">
            <div className="flex items-center gap-3">
              <input
                value={inviteInput}
                onChange={e => setInviteInput(e.target.value.toUpperCase())}
                placeholder={t('family.enterInviteCode')}
                maxLength={6}
                className="flex-1 bg-gray-700 border border-gray-600 rounded px-3 py-2 text-white font-mono tracking-widest uppercase placeholder:normal-case placeholder:tracking-normal"
                aria-label={t('family.enterInviteCode')}
                onKeyDown={e => { if (e.key === 'Enter') acceptInvite() }}
              />
              <button
                onClick={acceptInvite}
                disabled={accepting || !inviteInput.trim()}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
              >
                {t('family.acceptInvite')}
              </button>
            </div>
            {acceptError && (
              <p className="text-red-400 text-sm mt-2">{acceptError}</p>
            )}
          </div>
        </section>
      )}

      {/* Award Stars modal (admin only) */}
      {awardingForChild && (
        <div
          className="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby="award-stars-title"
        >
          <div className="bg-gray-800 border border-gray-700 rounded-xl w-full max-w-md p-6">
            <div className="flex items-center gap-2 mb-4">
              <Sparkles size={18} className="text-yellow-400" />
              <h2 id="award-stars-title" className="text-white font-semibold text-lg">
                {t('family.awardStars.title')}
              </h2>
              <span className="ml-auto text-gray-400 text-sm">
                {awardingForChild.nickname || awardingForChild.child_id}
              </span>
            </div>

            {awardSuccess ? (
              <div className="text-center py-4">
                <p className="text-green-400 font-medium">{t('family.awardStars.success')}</p>
                <button
                  onClick={closeAwardModal}
                  className="mt-4 px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
                >
                  {t('actions.close')}
                </button>
              </div>
            ) : (
              <div className="space-y-4">
                <div>
                  <label htmlFor="award-amount" className="block text-sm text-gray-400 mb-1">
                    {t('family.awardStars.amount')}
                  </label>
                  <input
                    id="award-amount"
                    type="number"
                    value={awardAmount}
                    onChange={e => setAwardAmount(e.target.value)}
                    placeholder={t('family.awardStars.amountPlaceholder')}
                    className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-white text-sm"
                  />
                </div>
                <div>
                  <label htmlFor="award-reason" className="block text-sm text-gray-400 mb-1">
                    {t('family.awardStars.reason')}
                  </label>
                  <input
                    id="award-reason"
                    type="text"
                    value={awardReason}
                    onChange={e => setAwardReason(e.target.value)}
                    placeholder={t('family.awardStars.reasonPlaceholder')}
                    className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-white text-sm"
                  />
                </div>
                <div>
                  <label htmlFor="award-description" className="block text-sm text-gray-400 mb-1">
                    {t('family.awardStars.description')}
                  </label>
                  <input
                    id="award-description"
                    type="text"
                    value={awardDescription}
                    onChange={e => setAwardDescription(e.target.value)}
                    placeholder={t('family.awardStars.descriptionPlaceholder')}
                    className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-white text-sm"
                  />
                </div>
                <div className="flex gap-2 pt-2">
                  <button
                    onClick={submitAward}
                    disabled={awarding || !awardAmount || parseInt(awardAmount, 10) === 0 || !awardReason.trim()}
                    className="flex-1 px-4 py-2 bg-yellow-600 hover:bg-yellow-500 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
                  >
                    {t('family.awardStars.submit')}
                  </button>
                  <button
                    onClick={closeAwardModal}
                    className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
                  >
                    {t('actions.cancel')}
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

interface WeeklyStatProps {
  label: string
  current: number
  previous: number
  icon?: React.ReactNode
}

function WeeklyStat({ label, current, previous, icon }: WeeklyStatProps) {
  const { t } = useTranslation('common')
  const diff = current - previous
  const TrendIcon = diff > 0 ? TrendingUp : diff < 0 ? TrendingDown : Minus
  const trendColor = diff > 0 ? 'text-green-400' : diff < 0 ? 'text-red-400' : 'text-gray-500'

  return (
    <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-4">
      <div className="flex items-center gap-2 mb-1">
        {icon}
        <p className="text-gray-400 text-sm">{label}</p>
      </div>
      <p className="text-2xl font-bold text-white">{formatNumber(current)}</p>
      <div className={`flex items-center gap-1 mt-1 text-xs ${trendColor}`}>
        <TrendIcon size={12} />
        <span>{t('family.trendDiff', { count: formatNumber(Math.abs(diff)) })}</span>
      </div>
    </div>
  )
}
