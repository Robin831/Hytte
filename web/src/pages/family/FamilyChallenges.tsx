import { useState, useEffect, useRef, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  Target, ArrowLeft, Plus, Trash2, Edit2, Check, X,
  CheckCircle, ChevronDown, ChevronUp, Star, UserPlus, UserMinus,
} from 'lucide-react'

interface Challenge {
  id: number
  creator_id: number
  title: string
  description: string
  challenge_type: string
  target_value: number
  star_reward: number
  start_date: string
  end_date: string
  is_active: boolean
  created_at: string
  updated_at: string
}

interface ChallengeParticipant {
  child_id: number
  nickname: string
  avatar_emoji: string
  added_at: string
  completed_at: string
}

interface FamilyChild {
  id: number
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface ChallengeForm {
  title: string
  description: string
  challenge_type: string
  target_value: string
  star_reward: string
  start_date: string
  end_date: string
  is_active: boolean
  selected_children: number[]
}

const EMPTY_FORM: ChallengeForm = {
  title: '',
  description: '',
  challenge_type: 'custom',
  target_value: '0',
  star_reward: '5',
  start_date: '',
  end_date: '',
  is_active: true,
  selected_children: [],
}

const CHALLENGE_TYPES = ['distance', 'duration', 'workout_count', 'streak', 'custom'] as const

function isExpired(c: Challenge): boolean {
  if (!c.end_date) return false
  const parts = c.end_date.split('-')
  if (parts.length !== 3) return false
  const [y, m, d] = parts.map(Number)
  const end = new Date(y, m - 1, d)
  return !isNaN(end.getTime()) && end < new Date()
}

export default function FamilyChallenges() {
  const { t } = useTranslation('common')
  const [challenges, setChallenges] = useState<Challenge[]>([])
  const [children, setChildren] = useState<FamilyChild[]>([])
  const [participantsMap, setParticipantsMap] = useState<Record<number, ChallengeParticipant[]>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [notification, setNotification] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [refresh, setRefresh] = useState(0)
  const notificationTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const [showCreateForm, setShowCreateForm] = useState(false)
  const [createForm, setCreateForm] = useState<ChallengeForm>(EMPTY_FORM)
  const [creating, setCreating] = useState(false)

  const [editingId, setEditingId] = useState<number | null>(null)
  const [editForm, setEditForm] = useState<ChallengeForm>(EMPTY_FORM)
  const [saving, setSaving] = useState(false)

  const [deleteConfirmId, setDeleteConfirmId] = useState<number | null>(null)
  const [pastOpen, setPastOpen] = useState(false)

  const showNotification = useCallback((message: string, type: 'success' | 'error') => {
    setNotification({ message, type })
    if (notificationTimerRef.current !== null) clearTimeout(notificationTimerRef.current)
    notificationTimerRef.current = setTimeout(() => {
      setNotification(null)
      notificationTimerRef.current = null
    }, 4000)
  }, [])

  useEffect(() => {
    return () => {
      if (notificationTimerRef.current !== null) clearTimeout(notificationTimerRef.current)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    const load = async () => {
      setLoading(true)
      setError(null)
      try {
        const [challengesRes, childrenRes] = await Promise.all([
          fetch('/api/family/challenges', { credentials: 'include', signal: controller.signal }),
          fetch('/api/family/children', { credentials: 'include', signal: controller.signal }),
        ])
        if (!challengesRes.ok || !childrenRes.ok) throw new Error('fetch failed')
        const [challengesData, childrenData] = await Promise.all([
          challengesRes.json() as Promise<{ challenges: Challenge[] }>,
          childrenRes.json() as Promise<{ children: FamilyChild[] }>,
        ])
        const fetchedChallenges = challengesData.challenges ?? []
        setChallenges(fetchedChallenges)
        setChildren(childrenData.children ?? [])

        // Fetch participants for each challenge in parallel.
        const results = await Promise.all(
          fetchedChallenges.map(c =>
            fetch(`/api/family/challenges/${c.id}/participants`, {
              credentials: 'include',
              signal: controller.signal,
            })
              .then(r => (r.ok ? r.json() : { participants: [] }))
              .then((d: { participants: ChallengeParticipant[] }) => ({
                id: c.id,
                participants: d.participants ?? [],
              }))
              .catch(() => ({ id: c.id, participants: [] as ChallengeParticipant[] }))
          )
        )
        const pMap: Record<number, ChallengeParticipant[]> = {}
        for (const { id, participants } of results) {
          pMap[id] = participants
        }
        setParticipantsMap(pMap)
      } catch (err: unknown) {
        if (controller.signal.aborted) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(t('family.challenges.errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    load()
    return () => controller.abort()
  }, [t, refresh])

  async function createChallenge() {
    if (!createForm.title.trim()) return
    setCreating(true)
    try {
      const res = await fetch('/api/family/challenges', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(buildPayload(createForm)),
      })
      if (!res.ok) throw new Error('failed')
      const data = (await res.json()) as { challenge: Challenge }
      const newID = data.challenge.id

      // Enroll selected children.
      await Promise.all(
        createForm.selected_children.map(childID =>
          fetch(`/api/family/challenges/${newID}/participants`, {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ child_id: childID }),
          })
        )
      )

      setCreateForm(EMPTY_FORM)
      setShowCreateForm(false)
      showNotification(t('family.challenges.success.created'), 'success')
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.challenges.errors.failedToCreate'), 'error')
    } finally {
      setCreating(false)
    }
  }

  async function saveEdit(id: number) {
    setSaving(true)
    try {
      const res = await fetch(`/api/family/challenges/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(buildPayload(editForm)),
      })
      if (!res.ok) throw new Error('failed')
      setEditingId(null)
      showNotification(t('family.challenges.success.updated'), 'success')
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.challenges.errors.failedToUpdate'), 'error')
    } finally {
      setSaving(false)
    }
  }

  async function deleteChallenge(id: number) {
    try {
      const res = await fetch(`/api/family/challenges/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('failed')
      setDeleteConfirmId(null)
      showNotification(t('family.challenges.success.deleted'), 'success')
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.challenges.errors.failedToDelete'), 'error')
    }
  }

  async function addParticipant(challengeID: number, childID: number) {
    try {
      const res = await fetch(`/api/family/challenges/${challengeID}/participants`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ child_id: childID }),
      })
      if (!res.ok) throw new Error('failed')
      showNotification(t('family.challenges.success.participantAdded'), 'success')
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.challenges.errors.failedToAddParticipant'), 'error')
    }
  }

  async function removeParticipant(challengeID: number, childID: number) {
    try {
      const res = await fetch(`/api/family/challenges/${challengeID}/participants/${childID}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('failed')
      showNotification(t('family.challenges.success.participantRemoved'), 'success')
      setRefresh(r => r + 1)
    } catch {
      showNotification(t('family.challenges.errors.failedToRemoveParticipant'), 'error')
    }
  }

  function buildPayload(form: ChallengeForm) {
    return {
      title: form.title.trim(),
      description: form.description,
      challenge_type: form.challenge_type,
      target_value: parseFloat(form.target_value) || 0,
      star_reward: parseInt(form.star_reward, 10) || 0,
      start_date: form.start_date,
      end_date: form.end_date,
      is_active: form.is_active,
    }
  }

  function startEdit(c: Challenge) {
    setEditingId(c.id)
    setEditForm({
      title: c.title,
      description: c.description,
      challenge_type: c.challenge_type,
      target_value: String(c.target_value),
      star_reward: String(c.star_reward),
      start_date: c.start_date,
      end_date: c.end_date,
      is_active: c.is_active,
      selected_children: [],
    })
  }

  const active = challenges.filter(c => c.is_active && !isExpired(c))
  const past = challenges.filter(c => !c.is_active || isExpired(c))

  if (loading) {
    return (
      <div className="p-6 max-w-3xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Target size={24} className="text-purple-400" />
          <h1 className="text-2xl font-semibold text-white">{t('family.challenges.title')}</h1>
        </div>
        <div className="space-y-4">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-28 rounded-xl bg-gray-800 animate-pulse" />
          ))}
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6 max-w-3xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Target size={24} className="text-purple-400" />
          <h1 className="text-2xl font-semibold text-white">{t('family.challenges.title')}</h1>
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
        <Target size={24} className="text-purple-400" />
        <h1 className="text-2xl font-semibold text-white">{t('family.challenges.title')}</h1>
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

      {/* Create button + form */}
      <section>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-lg font-semibold text-white">{t('family.challenges.active')}</h2>
          <button
            type="button"
            onClick={() => { setShowCreateForm(v => !v); setCreateForm(EMPTY_FORM) }}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-purple-600 hover:bg-purple-700 text-white text-sm rounded-lg transition-colors cursor-pointer"
          >
            <Plus size={14} />
            {t('family.challenges.createChallenge')}
          </button>
        </div>

        {showCreateForm && (
          <div className="mb-4 rounded-xl bg-gray-800/70 border border-gray-700 p-4 space-y-4">
            <h3 className="text-white font-medium text-sm">{t('family.challenges.createChallenge')}</h3>
            <ChallengeFormFields
              form={createForm}
              onChange={setCreateForm}
              children={children}
              idPrefix="create"
              showChildSelect
              t={t}
            />
            <div className="flex gap-2">
              <button
                type="button"
                onClick={createChallenge}
                disabled={creating || !createForm.title.trim()}
                className="px-4 py-2 bg-purple-600 hover:bg-purple-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
              >
                {creating ? '...' : t('family.challenges.form.create')}
              </button>
              <button
                type="button"
                onClick={() => setShowCreateForm(false)}
                className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
              >
                {t('actions.cancel')}
              </button>
            </div>
          </div>
        )}

        {active.length === 0 && !showCreateForm ? (
          <div className="rounded-xl bg-gray-800/50 border border-gray-700 p-8 text-center space-y-2">
            <p className="text-gray-300 font-medium">{t('family.challenges.noChallenges')}</p>
            <p className="text-gray-500 text-sm">{t('family.challenges.noChallengesHint')}</p>
          </div>
        ) : (
          <div className="space-y-3">
            {active.map(c => (
              <ChallengeCard
                key={c.id}
                challenge={c}
                participants={participantsMap[c.id] ?? []}
                children={children}
                editingId={editingId}
                editForm={editForm}
                setEditForm={setEditForm}
                saving={saving}
                deleteConfirmId={deleteConfirmId}
                setDeleteConfirmId={setDeleteConfirmId}
                onStartEdit={startEdit}
                onSaveEdit={saveEdit}
                onCancelEdit={() => setEditingId(null)}
                onDelete={deleteChallenge}
                onAddParticipant={addParticipant}
                onRemoveParticipant={removeParticipant}
                t={t}
              />
            ))}
          </div>
        )}
      </section>

      {/* Past challenges (collapsible) */}
      {past.length > 0 && (
        <section>
          <button
            type="button"
            onClick={() => setPastOpen(v => !v)}
            className="flex items-center gap-2 text-sm font-semibold text-gray-400 uppercase tracking-wide cursor-pointer hover:text-gray-300 transition-colors mb-3"
            aria-expanded={pastOpen}
          >
            {pastOpen ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
            {t('family.challenges.past')}
            <span className="text-xs normal-case font-normal text-gray-500">({past.length})</span>
          </button>
          {pastOpen && (
            <div className="space-y-3">
              {past.map(c => (
                <ChallengeCard
                  key={c.id}
                  challenge={c}
                  participants={participantsMap[c.id] ?? []}
                  children={children}
                  editingId={editingId}
                  editForm={editForm}
                  setEditForm={setEditForm}
                  saving={saving}
                  deleteConfirmId={deleteConfirmId}
                  setDeleteConfirmId={setDeleteConfirmId}
                  onStartEdit={startEdit}
                  onSaveEdit={saveEdit}
                  onCancelEdit={() => setEditingId(null)}
                  onDelete={deleteChallenge}
                  onAddParticipant={addParticipant}
                  onRemoveParticipant={removeParticipant}
                  t={t}
                  dimmed
                />
              ))}
            </div>
          )}
        </section>
      )}
    </div>
  )
}

interface ChallengeCardProps {
  challenge: Challenge
  participants: ChallengeParticipant[]
  children: FamilyChild[]
  editingId: number | null
  editForm: ChallengeForm
  setEditForm: (updater: (prev: ChallengeForm) => ChallengeForm) => void
  saving: boolean
  deleteConfirmId: number | null
  setDeleteConfirmId: (id: number | null) => void
  onStartEdit: (c: Challenge) => void
  onSaveEdit: (id: number) => void
  onCancelEdit: () => void
  onDelete: (id: number) => void
  onAddParticipant: (challengeID: number, childID: number) => void
  onRemoveParticipant: (challengeID: number, childID: number) => void
  t: ReturnType<typeof import('react-i18next').useTranslation<'common'>>['t']
  dimmed?: boolean
}

function ChallengeCard({
  challenge,
  participants,
  children,
  editingId,
  editForm,
  setEditForm,
  saving,
  deleteConfirmId,
  setDeleteConfirmId,
  onStartEdit,
  onSaveEdit,
  onCancelEdit,
  onDelete,
  onAddParticipant,
  onRemoveParticipant,
  t,
  dimmed,
}: ChallengeCardProps) {
  const isEditing = editingId === challenge.id

  const enrolledIDs = new Set(participants.map(p => p.child_id))
  const unenrolledChildren = children.filter(c => !enrolledIDs.has(c.child_id))

  return (
    <div
      className={`rounded-xl border p-4 space-y-3 ${
        dimmed
          ? 'bg-gray-800/30 border-gray-700/50 opacity-70'
          : 'bg-gray-800/60 border-gray-700'
      }`}
    >
      {isEditing ? (
        <div className="space-y-4">
          <ChallengeFormFields
            form={editForm}
            onChange={setEditForm}
            children={children}
            idPrefix={`edit-${challenge.id}`}
            showChildSelect={false}
            t={t}
          />
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => onSaveEdit(challenge.id)}
              disabled={saving || !editForm.title.trim()}
              className="flex items-center gap-1.5 px-3 py-1.5 bg-purple-600 hover:bg-purple-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
            >
              <Check size={14} />
              {t('family.challenges.form.update')}
            </button>
            <button
              type="button"
              onClick={onCancelEdit}
              className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
            >
              <X size={14} />
              {t('actions.cancel')}
            </button>
          </div>
        </div>
      ) : (
        <>
          {/* Title row */}
          <div className="flex items-start gap-2">
            <div className="flex-1 min-w-0">
              <h3 className="text-white font-semibold text-sm">{challenge.title}</h3>
              {challenge.description && (
                <p className="text-gray-400 text-xs mt-0.5">{challenge.description}</p>
              )}
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <span className="flex items-center gap-1 text-yellow-400 font-bold text-xs">
                <Star size={12} className="fill-yellow-400" />
                {challenge.star_reward}
              </span>
              <button
                type="button"
                onClick={() => onStartEdit(challenge)}
                className="p-1.5 text-gray-400 hover:text-white transition-colors cursor-pointer"
                aria-label={t('actions.edit')}
              >
                <Edit2 size={14} aria-hidden="true" />
              </button>
              <button
                type="button"
                onClick={() => setDeleteConfirmId(challenge.id)}
                className="p-1.5 text-gray-400 hover:text-red-400 transition-colors cursor-pointer"
                aria-label={t('actions.delete')}
              >
                <Trash2 size={14} aria-hidden="true" />
              </button>
            </div>
          </div>

          {/* Meta row */}
          <div className="flex flex-wrap gap-2 text-xs">
            <span className="px-2 py-0.5 rounded-full bg-purple-500/20 text-purple-300 border border-purple-500/30">
              {t(`family.challenges.form.types.${challenge.challenge_type}` as Parameters<typeof t>[0])}
            </span>
            {challenge.target_value > 0 && (
              <span className="text-gray-400">
                {t('family.challenges.form.target')}: {challenge.target_value}
              </span>
            )}
            {challenge.start_date && (
              <span className="text-gray-400">{challenge.start_date}</span>
            )}
            {challenge.end_date && (
              <span className="text-gray-400">→ {challenge.end_date}</span>
            )}
          </div>

          {/* Participants */}
          <div className="space-y-2">
            <p className="text-xs font-medium text-gray-400 uppercase tracking-wide">
              {t('family.challenges.participants.title')}
            </p>
            {participants.length === 0 ? (
              <p className="text-xs text-gray-500">{t('family.challenges.participants.none')}</p>
            ) : (
              <div className="space-y-1">
                {participants.map(p => (
                  <div key={p.child_id} className="flex items-center gap-2">
                    <span className="text-sm" role="img" aria-hidden="true">
                      {p.avatar_emoji || '⭐'}
                    </span>
                    <span className="text-sm text-gray-300 flex-1">
                      {p.nickname || t('family.unknownChild', { id: p.child_id })}
                    </span>
                    {p.completed_at ? (
                      <span className="flex items-center gap-1 text-xs text-green-400">
                        <CheckCircle size={12} />
                        {t('family.challenges.participants.completed')}
                      </span>
                    ) : (
                      <span className="text-xs text-gray-500">
                        {t('family.challenges.participants.inProgress')}
                      </span>
                    )}
                    <button
                      type="button"
                      onClick={() => onRemoveParticipant(challenge.id, p.child_id)}
                      className="p-1 text-gray-500 hover:text-red-400 transition-colors cursor-pointer"
                      aria-label={t('family.challenges.participants.remove')}
                    >
                      <UserMinus size={12} aria-hidden="true" />
                    </button>
                  </div>
                ))}
              </div>
            )}

            {/* Add unenrolled children */}
            {unenrolledChildren.length > 0 && (
              <div className="flex flex-wrap gap-2 pt-1">
                {unenrolledChildren.map(child => (
                  <button
                    key={child.child_id}
                    type="button"
                    onClick={() => onAddParticipant(challenge.id, child.child_id)}
                    className="flex items-center gap-1 px-2 py-1 text-xs rounded-lg bg-gray-700 hover:bg-gray-600 text-gray-300 transition-colors cursor-pointer border border-gray-600"
                    aria-label={t('family.challenges.participants.add')}
                  >
                    <UserPlus size={11} aria-hidden="true" />
                    {child.nickname || t('family.unknownChild', { id: child.child_id })}
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Delete confirm */}
          {deleteConfirmId === challenge.id && (
            <div className="pt-2 border-t border-gray-700 space-y-2">
              <p className="text-sm text-red-400">{t('family.challenges.deleteConfirm')}</p>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => onDelete(challenge.id)}
                  className="px-3 py-1 bg-red-700 hover:bg-red-600 text-white text-xs rounded transition-colors cursor-pointer"
                >
                  {t('actions.confirm')}
                </button>
                <button
                  type="button"
                  onClick={() => setDeleteConfirmId(null)}
                  className="px-3 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs rounded transition-colors cursor-pointer"
                >
                  {t('actions.cancel')}
                </button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

interface ChallengeFormFieldsProps {
  form: ChallengeForm
  onChange: (updater: (prev: ChallengeForm) => ChallengeForm) => void
  children: FamilyChild[]
  idPrefix: string
  showChildSelect: boolean
  t: ReturnType<typeof import('react-i18next').useTranslation<'common'>>['t']
}

function ChallengeFormFields({ form, onChange, children, idPrefix, showChildSelect, t }: ChallengeFormFieldsProps) {
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div className="sm:col-span-2">
          <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-title`}>
            {t('family.challenges.form.title')} *
          </label>
          <input
            id={`${idPrefix}-title`}
            value={form.title}
            onChange={e => onChange(f => ({ ...f, title: e.target.value }))}
            placeholder={t('family.challenges.form.titlePlaceholder')}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
          />
        </div>

        <div className="sm:col-span-2">
          <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-desc`}>
            {t('family.challenges.form.description')}
          </label>
          <input
            id={`${idPrefix}-desc`}
            value={form.description}
            onChange={e => onChange(f => ({ ...f, description: e.target.value }))}
            placeholder={t('family.challenges.form.descriptionPlaceholder')}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
          />
        </div>

        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-type`}>
            {t('family.challenges.form.type')}
          </label>
          <select
            id={`${idPrefix}-type`}
            value={form.challenge_type}
            onChange={e => onChange(f => ({ ...f, challenge_type: e.target.value }))}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
          >
            {CHALLENGE_TYPES.map(type => (
              <option key={type} value={type}>
                {t(`family.challenges.form.types.${type}` as Parameters<typeof t>[0])}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-target`}>
            {t('family.challenges.form.targetValue')}
          </label>
          <input
            id={`${idPrefix}-target`}
            type="number"
            min={0}
            step="0.1"
            value={form.target_value}
            onChange={e => onChange(f => ({ ...f, target_value: e.target.value }))}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
          />
        </div>

        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-reward`}>
            {t('family.challenges.form.starReward')}
          </label>
          <input
            id={`${idPrefix}-reward`}
            type="number"
            min={0}
            value={form.star_reward}
            onChange={e => onChange(f => ({ ...f, star_reward: e.target.value }))}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
          />
        </div>

        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-active`}>
            {t('family.challenges.form.isActive')}
          </label>
          <input
            id={`${idPrefix}-active`}
            type="checkbox"
            checked={form.is_active}
            onChange={e => onChange(f => ({ ...f, is_active: e.target.checked }))}
            className="w-4 h-4 rounded accent-purple-500"
          />
        </div>

        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-start`}>
            {t('family.challenges.form.startDate')}
          </label>
          <input
            id={`${idPrefix}-start`}
            type="date"
            value={form.start_date}
            onChange={e => onChange(f => ({ ...f, start_date: e.target.value }))}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
          />
        </div>

        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor={`${idPrefix}-end`}>
            {t('family.challenges.form.endDate')}
          </label>
          <input
            id={`${idPrefix}-end`}
            type="date"
            value={form.end_date}
            onChange={e => onChange(f => ({ ...f, end_date: e.target.value }))}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white text-sm"
          />
        </div>
      </div>

      {showChildSelect && children.length > 0 && (
        <div>
          <p className="text-xs text-gray-400 mb-2">{t('family.challenges.form.participants')}</p>
          <div className="flex flex-wrap gap-2">
            {children.map(child => {
              const selected = form.selected_children.includes(child.child_id)
              return (
                <button
                  key={child.child_id}
                  type="button"
                  onClick={() =>
                    onChange(f => ({
                      ...f,
                      selected_children: selected
                        ? f.selected_children.filter(id => id !== child.child_id)
                        : [...f.selected_children, child.child_id],
                    }))
                  }
                  className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm transition-colors cursor-pointer border ${
                    selected
                      ? 'bg-purple-600 border-purple-500 text-white'
                      : 'bg-gray-700 border-gray-600 text-gray-300 hover:bg-gray-600'
                  }`}
                >
                  <span role="img" aria-hidden="true">{child.avatar_emoji || '⭐'}</span>
                  {child.nickname || t('family.unknownChild', { id: child.child_id })}
                </button>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
