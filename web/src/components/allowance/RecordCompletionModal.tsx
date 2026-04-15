import { useState, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../ui/dialog'

interface FamilyChild {
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface RecordCompletionModalProps {
  open: boolean
  onClose: () => void
  onSuccess: () => void
  onToast: (message: string, type: 'success' | 'error') => void
  choreId: number
  choreName: string
  choreIcon: string
  assignedChildId: number | null
  completionMode: 'solo' | 'team'
  minTeamSize: number
  children: FamilyChild[]
}

interface RecordCompletionFormProps {
  onClose: () => void
  onSuccess: () => void
  onToast: (message: string, type: 'success' | 'error') => void
  choreId: number
  assignedChildId: number | null
  completionMode: 'solo' | 'team'
  minTeamSize: number
  children: FamilyChild[]
  titleId: string
  choreName: string
  choreIcon: string
}

function localToday() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function RecordCompletionForm({
  onClose,
  onSuccess,
  onToast,
  choreId,
  assignedChildId,
  completionMode,
  minTeamSize,
  children,
  titleId,
  choreName,
  choreIcon,
}: RecordCompletionFormProps) {
  const { t } = useTranslation('allowance')

  const [selectedChildIds, setSelectedChildIds] = useState<Set<number>>(
    () => assignedChildId != null ? new Set([assignedChildId]) : new Set()
  )
  const [date, setDate] = useState(localToday)
  const [notes, setNotes] = useState('')
  const [status, setStatus] = useState<'approved' | 'pending'>('approved')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const isTeam = completionMode === 'team'
  const teamTooSmall = isTeam && selectedChildIds.size < minTeamSize
  const noChildren = selectedChildIds.size === 0

  function toggleChild(childId: number) {
    setSelectedChildIds(prev => {
      const next = new Set(prev)
      if (next.has(childId)) {
        next.delete(childId)
      } else {
        next.add(childId)
      }
      return next
    })
  }

  async function handleSubmit() {
    if (noChildren) {
      setError(t('errors.noChildrenSelected'))
      return
    }
    if (teamTooSmall) {
      return
    }

    setSubmitting(true)
    setError('')
    try {
      const res = await fetch(`/api/allowance/chores/${choreId}/record`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          child_ids: Array.from(selectedChildIds),
          date,
          notes: notes.trim() || undefined,
          status,
        }),
      })
      if (!res.ok) {
        throw new Error()
      }

      const data: { completions: unknown[]; skipped?: number[] } = await res.json()

      const completedNames = children
        .filter(c => selectedChildIds.has(c.child_id) && !(data.skipped ?? []).includes(c.child_id))
        .map(c => c.nickname)

      if (completedNames.length > 0) {
        onToast(t('record.successToast', { names: completedNames.join(', ') }), 'success')
      }

      for (const skippedId of data.skipped ?? []) {
        const child = children.find(c => c.child_id === skippedId)
        if (child) {
          onToast(t('record.skippedWarning', { name: child.nickname }), 'error')
        }
      }

      onClose()
      onSuccess()
    } catch {
      setError(t('errors.actionFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <DialogHeader
        id={titleId}
        title={`${t('record.title')} — ${choreIcon} ${choreName}`}
        onClose={onClose}
      />
      <DialogBody>
        <div className="space-y-4">
          <div>
            <label className="block text-sm text-gray-400 mb-2">{t('record.children')}</label>
            <div className="flex flex-wrap gap-2">
              {children.map(child => {
                const selected = selectedChildIds.has(child.child_id)
                return (
                  <button
                    key={child.child_id}
                    type="button"
                    onClick={() => toggleChild(child.child_id)}
                    className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm font-medium transition-colors cursor-pointer
                      ${selected
                        ? 'bg-blue-600 text-white'
                        : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
                      }`}
                  >
                    <span>{child.avatar_emoji || '⭐'}</span>
                    {child.nickname}
                  </button>
                )
              })}
            </div>
            {teamTooSmall && (
              <p className="text-amber-400 text-xs mt-2">
                {t('errors.teamTooSmall', { count: minTeamSize })}
              </p>
            )}
          </div>

          <div>
            <label htmlFor="record-date" className="block text-sm text-gray-400 mb-1">
              {t('record.date')}
            </label>
            <input
              id="record-date"
              type="date"
              value={date}
              onChange={e => setDate(e.target.value)}
              className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>

          <div>
            <label htmlFor="record-notes" className="block text-sm text-gray-400 mb-1">
              {t('record.notes')}
            </label>
            <textarea
              id="record-notes"
              value={notes}
              onChange={e => setNotes(e.target.value)}
              rows={2}
              className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 resize-none"
              placeholder={t('record.notesPlaceholder')}
            />
          </div>

          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => setStatus('approved')}
              className={`flex-1 py-2 rounded-lg text-sm font-medium transition-colors cursor-pointer
                ${status === 'approved'
                  ? 'bg-green-600 text-white'
                  : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
                }`}
            >
              {t('record.approved')}
            </button>
            <button
              type="button"
              onClick={() => setStatus('pending')}
              className={`flex-1 py-2 rounded-lg text-sm font-medium transition-colors cursor-pointer
                ${status === 'pending'
                  ? 'bg-amber-600 text-white'
                  : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
                }`}
            >
              {t('record.pending')}
            </button>
          </div>

          {error && <p className="text-red-400 text-sm">{error}</p>}
        </div>
      </DialogBody>
      <DialogFooter>
        <button
          type="button"
          onClick={onClose}
          disabled={submitting}
          className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors disabled:opacity-50"
        >
          {t('actions.cancel')}
        </button>
        <button
          type="button"
          onClick={handleSubmit}
          disabled={submitting || noChildren || teamTooSmall}
          className="px-4 py-2 text-sm font-medium rounded bg-blue-600 hover:bg-blue-500 text-white transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {submitting ? t('record.submitting') : t('record.submit')}
        </button>
      </DialogFooter>
    </>
  )
}

export default function RecordCompletionModal({
  open,
  onClose,
  onSuccess,
  onToast,
  choreId,
  choreName,
  choreIcon,
  assignedChildId,
  completionMode,
  minTeamSize,
  children,
}: RecordCompletionModalProps) {
  const titleId = useId()

  return (
    <Dialog open={open} onClose={onClose} aria-labelledby={titleId}>
      {open && (
        <RecordCompletionForm
          onClose={onClose}
          onSuccess={onSuccess}
          onToast={onToast}
          choreId={choreId}
          choreName={choreName}
          choreIcon={choreIcon}
          assignedChildId={assignedChildId}
          completionMode={completionMode}
          minTeamSize={minTeamSize}
          children={children}
          titleId={titleId}
        />
      )}
    </Dialog>
  )
}
