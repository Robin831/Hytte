import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, Pencil } from 'lucide-react'
import { formatDate, formatDateTime } from '../../utils/formatDate'
import { EditNoteDialog } from './EditNoteDialog'
import type { Note, NoteScope, StrideMutationResult } from '../../types/stride'
import type { NoteInput } from '../../hooks/useStrideNotes'

function localDateKey(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function noteScopeBadgeClass(scope: NoteScope): string {
  switch (scope) {
    case 'nightly':
      return 'bg-indigo-500/15 text-indigo-300 border border-indigo-500/30'
    case 'weekly':
      return 'bg-purple-500/15 text-purple-300 border border-purple-500/30'
    case 'any':
    default:
      return 'bg-gray-500/15 text-gray-300 border border-gray-500/30'
  }
}

interface CoachNotesSectionProps {
  notes: Note[]
  consumedNotes: Note[]
  loading: boolean
  onCreate: (input: NoteInput) => Promise<StrideMutationResult>
  onUpdate: (id: number, input: NoteInput) => Promise<StrideMutationResult>
  onDelete: (id: number) => Promise<StrideMutationResult>
  /** Re-reads the notes after a successful create or update. */
  onReload: () => Promise<void>
}

export function CoachNotesSection({
  notes, consumedNotes, loading, onCreate, onUpdate, onDelete, onReload,
}: CoachNotesSectionProps) {
  const { t } = useTranslation('stride')

  // Note form state
  const [noteContent, setNoteContent] = useState('')
  const [noteTargetDate, setNoteTargetDate] = useState(() => localDateKey(new Date()))
  const [noteScope, setNoteScope] = useState<NoteScope>('any')
  const [noteSubmitting, setNoteSubmitting] = useState(false)

  // Note edit modal state
  const [editingNote, setEditingNote] = useState<Note | null>(null)
  const [editContent, setEditContent] = useState('')
  const [editTargetDate, setEditTargetDate] = useState('')
  const [editScope, setEditScope] = useState<NoteScope>('any')
  const [editSubmitting, setEditSubmitting] = useState(false)
  const [editError, setEditError] = useState('')
  const [olderNotesOpen, setOlderNotesOpen] = useState(false)

  async function handleCreateNote(e: React.FormEvent) {
    e.preventDefault()
    if (!noteContent.trim()) return
    setNoteSubmitting(true)
    try {
      const result = await onCreate({ content: noteContent, target_date: noteTargetDate, scope: noteScope })
      if (!result.ok) return
      setNoteContent('')
      setNoteScope('any')
      await onReload()
    } finally {
      setNoteSubmitting(false)
    }
  }

  function openEditNote(note: Note) {
    setEditingNote(note)
    setEditContent(note.content)
    setEditTargetDate(note.target_date)
    setEditScope(note.scope ?? 'any')
    setEditError('')
  }

  function closeEditNote() {
    setEditingNote(null)
    setEditError('')
  }

  async function handleEditNoteSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!editingNote) return
    if (!editContent.trim()) return
    setEditSubmitting(true)
    setEditError('')
    try {
      const result = await onUpdate(editingNote.id, {
        content: editContent,
        target_date: editTargetDate,
        scope: editScope,
      })
      if (!result.ok) {
        if (result.status === 409) {
          setEditError(t('notes.editError.consumed'))
        } else {
          setEditError(result.error ?? t('notes.editError.generic'))
        }
        return
      }
      closeEditNote()
      await onReload()
    } finally {
      setEditSubmitting(false)
    }
  }

  function handleDeleteNote(id: number) {
    void onDelete(id)
  }

  // Partition notes into "active" (target_date within the last 7 days or in the future)
  // and "older" so the Coach Notes section stays compact when weeks of history accumulate.
  // Subtract 6 days so the window is exactly 7 calendar days inclusive (today + prior 6).
  // Depend on `today` so the cutoff updates if the section stays mounted across a day boundary.
  const today = localDateKey(new Date())
  const noteRecentCutoff = useMemo(() => {
    const d = new Date()
    d.setDate(d.getDate() - 6)
    return localDateKey(d)
  }, [today])

  const { activeNotes, olderNotes, activeConsumedNotes, olderConsumedNotes } = useMemo(() => {
    const isRecent = (n: Note) => (n.target_date ?? '') >= noteRecentCutoff
    return {
      activeNotes: notes.filter(isRecent),
      olderNotes: notes.filter(n => !isRecent(n)),
      activeConsumedNotes: consumedNotes.filter(isRecent),
      olderConsumedNotes: consumedNotes.filter(n => !isRecent(n)),
    }
  }, [notes, consumedNotes, noteRecentCutoff])
  const olderNoteCount = olderNotes.length + olderConsumedNotes.length

  const renderActiveNote = (note: Note) => {
    const scope: NoteScope = note.scope ?? 'any'
    return (
      <div key={note.id} className="flex items-start gap-3 p-3 bg-gray-800 rounded-xl border border-gray-700 group">
        <div className="flex-1 min-w-0 space-y-1">
          <p className="text-sm text-gray-200 whitespace-pre-wrap">{note.content}</p>
          <span className={`inline-flex items-center px-1.5 py-0.5 text-xs rounded ${noteScopeBadgeClass(scope)}`}>
            {t(`notes.scope.${scope}`)}
          </span>
        </div>
        <div className="flex-shrink-0 flex flex-col items-end gap-1">
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => openEditNote(note)}
              className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-blue-400 transition-all"
              aria-label={t('notes.edit')}
            >
              <Pencil size={14} />
            </button>
            <button
              type="button"
              onClick={() => handleDeleteNote(note.id)}
              className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
              aria-label={t('notes.delete')}
            >
              <Trash2 size={14} />
            </button>
          </div>
          <span className="text-xs text-gray-500">
            {note.target_date && <span className="mr-1">{formatDate(`${note.target_date}T00:00:00`)}</span>}
            {formatDateTime(note.created_at, { dateStyle: 'short', timeStyle: 'short' })}
          </span>
        </div>
      </div>
    )
  }

  const renderConsumedNote = (note: Note) => {
    const scope: NoteScope = note.scope ?? 'any'
    const consumedByLabel = note.consumed_by === 'nightly'
      ? t('notes.consumedByProcess.nightly')
      : note.consumed_by === 'weekly'
        ? t('notes.consumedByProcess.weekly')
        : note.consumed_by === 'manual'
          ? t('notes.consumedByProcess.manual')
          : null
    const consumedDate = note.consumed_at ? formatDate(note.consumed_at) : null
    return (
      <div key={note.id} className="flex items-start gap-3 p-3 bg-gray-800/60 rounded-xl border border-gray-700/50 group">
        <div className="flex-1 min-w-0 space-y-1">
          <p className="text-sm text-gray-400 whitespace-pre-wrap">{note.content}</p>
          <span className={`inline-flex items-center px-1.5 py-0.5 text-xs rounded ${noteScopeBadgeClass(scope)} opacity-70`}>
            {t(`notes.scope.${scope}`)}
          </span>
        </div>
        <div className="flex-shrink-0 flex flex-col items-end gap-1">
          <button
            type="button"
            onClick={() => handleDeleteNote(note.id)}
            className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
            aria-label={t('notes.delete')}
          >
            <Trash2 size={14} />
          </button>
          <div className="flex flex-col items-end gap-0.5">
            {consumedByLabel && consumedDate && (
              <span className="inline-flex items-center gap-1 px-1.5 py-0.5 text-xs rounded bg-gray-700/50 text-gray-400">
                {t('notes.consumedBy', { process: consumedByLabel, date: consumedDate })}
              </span>
            )}
            <span className="text-xs text-gray-500">
              {note.target_date && <span className="mr-1">{formatDate(`${note.target_date}T00:00:00`)}</span>}
              {formatDateTime(note.created_at, { dateStyle: 'short', timeStyle: 'short' })}
            </span>
          </div>
        </div>
      </div>
    )
  }

  return (
    <section>
      <h2 className="text-lg font-semibold text-white mb-4">{t('notes.title')}</h2>
      <form onSubmit={handleCreateNote} className="mb-4">
        <textarea
          value={noteContent}
          onChange={e => setNoteContent(e.target.value)}
          placeholder={t('notes.placeholder')}
          aria-label={t('notes.title')}
          rows={3}
          className="w-full bg-gray-800 border border-gray-700 rounded-xl px-4 py-3 text-base sm:text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500 resize-none"
        />
        <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-2">
              <label htmlFor="note-target-date" className="text-xs text-gray-400">{t('notes.targetDate')}</label>
              <input
                id="note-target-date"
                type="date"
                value={noteTargetDate}
                onChange={e => setNoteTargetDate(e.target.value)}
                className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500"
              />
            </div>
            <div className="flex items-center gap-2">
              <label htmlFor="note-scope" className="text-xs text-gray-400">{t('notes.scopeLabel')}</label>
              <select
                id="note-scope"
                value={noteScope}
                onChange={e => setNoteScope(e.target.value as NoteScope)}
                className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500"
              >
                <option value="any">{t('notes.scope.any')}</option>
                <option value="nightly">{t('notes.scope.nightly')}</option>
                <option value="weekly">{t('notes.scope.weekly')}</option>
              </select>
            </div>
          </div>
          <button
            type="submit"
            disabled={noteSubmitting || !noteContent.trim()}
            className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-lg transition-colors"
          >
            {noteSubmitting ? t('notes.saving') : t('notes.add')}
          </button>
        </div>
      </form>

      {loading ? (
        <p className="text-sm text-gray-400">{t('loading')}</p>
      ) : notes.length === 0 && consumedNotes.length === 0 ? (
        <p className="text-sm text-gray-500">{t('notes.empty')}</p>
      ) : (
        <>
          {activeNotes.length > 0 && (
            <>
              <h3 className="text-sm font-medium text-gray-400 mb-2">{t('notes.activeLabel')}</h3>
              <div className="space-y-2 mb-4">
                {activeNotes.map(renderActiveNote)}
              </div>
            </>
          )}

          {activeConsumedNotes.length > 0 && (
            <>
              <h3 className="text-sm font-medium text-gray-400 mb-2">{t('notes.historyLabel')}</h3>
              <div className="space-y-2">
                {activeConsumedNotes.map(renderConsumedNote)}
              </div>
            </>
          )}

          {olderNoteCount > 0 && (
            <details
              className="mt-4"
              open={olderNotesOpen}
            >
              <summary
                className="text-sm text-gray-500 cursor-pointer hover:text-gray-300"
                onClick={e => { e.preventDefault(); setOlderNotesOpen(prev => !prev) }}
              >
                {t('notes.olderNotes', { count: olderNoteCount })}
              </summary>
              <div className="mt-2 space-y-2">
                {olderNotes.map(renderActiveNote)}
                {olderConsumedNotes.map(renderConsumedNote)}
              </div>
            </details>
          )}
        </>
      )}

      {/* Edit-note modal */}
      <EditNoteDialog
        open={editingNote !== null}
        onClose={closeEditNote}
        onSubmit={handleEditNoteSubmit}
        content={editContent}
        onContentChange={setEditContent}
        targetDate={editTargetDate}
        onTargetDateChange={setEditTargetDate}
        scope={editScope}
        onScopeChange={setEditScope}
        submitting={editSubmitting}
        error={editError}
      />
    </section>
  )
}
