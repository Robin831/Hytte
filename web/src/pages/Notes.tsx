import { useCallback, useEffect, useRef, useState } from 'react'
import { Plus, Search, FileText } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '../components/ui/skeleton'
import { ConfirmDialog } from '../components/ui/dialog'
import { useDebouncedValue } from '../hooks/useDebouncedValue'
import { timeAgo } from '../utils/timeAgo'
import NoteEditor, { type NoteEditorHandle } from '../components/notes/NoteEditor'
import { useNotes, type Note, type NoteInput } from '../hooks/useNotes'

/** True when the event target is a field where `/` should type literally. */
function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || target.isContentEditable
}

export default function Notes() {
  const { t } = useTranslation('notes')
  const { t: tCommon } = useTranslation('common')
  const [search, setSearch] = useState('')
  // Debounce the search term so typing only fires one request after a pause,
  // not one per keystroke. The input stays bound to `search` for instant text.
  const debouncedSearch = useDebouncedValue(search, 250)
  const [activeTag, setActiveTag] = useState('')
  const [selectedNote, setSelectedNote] = useState<Note | null>(null)
  const [isCreating, setIsCreating] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Note | null>(null)
  // True when the open editor holds unsaved changes (reported by NoteEditor).
  const [isDirty, setIsDirty] = useState(false)
  // Holds the transition to run if the user confirms discarding their draft.
  // Stored as `() => fn` so React doesn't treat it as a lazy state initializer.
  const [pendingAction, setPendingAction] = useState<(() => void) | null>(null)
  const showDiscardConfirm = pendingAction !== null

  const { notes, allTags, loading, error, save, remove, clearError } = useNotes(debouncedSearch, activeTag)

  const searchInputRef = useRef<HTMLInputElement>(null)
  const editorRef = useRef<NoteEditorHandle>(null)

  function doOpenNote(note: Note) {
    setSelectedNote(note)
    setIsCreating(false)
    clearError()
  }

  const doStartCreating = useCallback(() => {
    setSelectedNote(null)
    setIsCreating(true)
    clearError()
  }, [clearError])

  // Keyboard shortcuts: Cmd/Ctrl+S saves the open note (suppressing the
  // browser's native save dialog), Cmd/Ctrl+N starts a new note, and `/`
  // focuses search unless the user is already typing in a field/editor.
  // The listener lives on `window` only while this page is mounted.
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.repeat) return
      const mod = e.metaKey || e.ctrlKey
      if (mod && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 's') {
        e.preventDefault()
        editorRef.current?.save()
        return
      }
      if (mod && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'n') {
        e.preventDefault()
        startCreating()
        return
      }
      if (e.key === '/' && !mod && !e.shiftKey && !e.altKey && !isTypingTarget(e.target)) {
        e.preventDefault()
        searchInputRef.current?.focus()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [startCreating])

  function doCloseEditor() {
    setSelectedNote(null)
    setIsCreating(false)
    clearError()
  }

  // Run `action` immediately when the draft is clean, otherwise stash it behind
  // the discard-changes confirmation dialog so unsaved work isn't lost silently.
  function guardDirty(action: () => void) {
    if (isDirty) {
      setPendingAction(() => action)
    } else {
      action()
    }
  }

  function openNote(note: Note) {
    guardDirty(() => doOpenNote(note))
  }

  function startCreating() {
    guardDirty(doStartCreating)
  }

  function closeEditor() {
    guardDirty(doCloseEditor)
  }

  function confirmDiscard() {
    pendingAction?.()
    setPendingAction(null)
  }

  async function handleSave(input: NoteInput): Promise<Note | null> {
    const saved = await save(input)
    if (saved) {
      setSelectedNote(saved)
      setIsCreating(false)
    }
    return saved
  }

  async function handleDelete(note: Note) {
    const ok = await remove(note.id)
    if (ok && selectedNote?.id === note.id) {
      setSelectedNote(null)
      setIsCreating(false)
    }
  }

  return (
    <>
    <ConfirmDialog
      open={deleteTarget !== null}
      onClose={() => setDeleteTarget(null)}
      onConfirm={() => deleteTarget && handleDelete(deleteTarget)}
      title={t('editor.deleteNote')}
      message={deleteTarget ? t('confirmDelete', { title: deleteTarget.title || t('untitled') }) : undefined}
    />
    <ConfirmDialog
      open={showDiscardConfirm}
      onClose={() => setPendingAction(null)}
      onConfirm={confirmDiscard}
      title={t('discardConfirm.title')}
      message={t('discardConfirm.message')}
      confirmLabel={t('discardConfirm.confirm')}
      cancelLabel={t('discardConfirm.cancel')}
    />
    <div className="flex h-[calc(100vh-3.5rem)] md:h-screen overflow-hidden">
      {/* Left panel — note list */}
      <aside className="w-72 shrink-0 bg-gray-950 border-r border-gray-800 flex flex-col">
        {/* Search + new */}
        <div className="p-3 border-b border-gray-800 space-y-2">
          <div className="flex gap-2">
            <div className="relative flex-1">
              <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-500" />
              <input
                ref={searchInputRef}
                type="text"
                placeholder={t('searchPlaceholder')}
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="w-full pl-8 pr-3 py-1.5 bg-gray-800 border border-gray-700 rounded text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                aria-label={t('searchLabel')}
                title={t('shortcuts.focusSearch')}
              />
            </div>
            <button
              onClick={startCreating}
              className="flex items-center gap-1 px-2 py-1.5 bg-blue-600 hover:bg-blue-500 text-white rounded text-sm transition-colors cursor-pointer shrink-0"
              title={t('shortcuts.newNote')}
              aria-label={t('newNote')}
            >
              <Plus size={16} />
            </button>
          </div>

          {/* Tag filters */}
          {allTags.length > 0 && (
            <div className="flex flex-wrap gap-1">
              <button
                onClick={() => setActiveTag('')}
                className={`px-2 py-0.5 rounded text-xs transition-colors cursor-pointer ${
                  activeTag === ''
                    ? 'bg-blue-600 text-white'
                    : 'bg-gray-800 text-gray-400 hover:text-white'
                }`}
              >
                {t('tagAll')}
              </button>
              {allTags.map(tag => (
                <button
                  key={tag}
                  onClick={() => setActiveTag(activeTag === tag ? '' : tag)}
                  className={`px-2 py-0.5 rounded text-xs transition-colors cursor-pointer ${
                    activeTag === tag
                      ? 'bg-blue-600 text-white'
                      : 'bg-gray-800 text-gray-400 hover:text-white'
                  }`}
                >
                  {tag}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Note list */}
        <div className="flex-1 overflow-y-auto">
          {loading ? (
            <div className="p-4 space-y-3" role="status" aria-live="polite">
              <p className="sr-only">{t('loading')}</p>
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-3/4" />
            </div>
          ) : notes.length === 0 ? (
            <div className="p-4 text-center">
              <FileText size={32} className="mx-auto text-gray-700 mb-2" />
              <p className="text-gray-500 text-sm">{t('empty.message')}</p>
              <button
                onClick={startCreating}
                className="mt-2 text-blue-400 hover:text-blue-300 text-sm underline cursor-pointer"
              >
                {t('empty.createFirst')}
              </button>
            </div>
          ) : (
            notes.map(note => (
              <button
                key={note.id}
                onClick={() => openNote(note)}
                className={`w-full text-left px-3 py-2.5 border-b border-gray-800/50 hover:bg-gray-800/50 transition-colors cursor-pointer ${
                  selectedNote?.id === note.id ? 'bg-gray-800' : ''
                }`}
              >
                <div className="flex items-baseline gap-2">
                  <p className="text-sm font-medium text-white truncate min-w-0 flex-1">
                    {note.title || <span className="text-gray-500 italic">{t('untitled')}</span>}
                  </p>
                  <span className="text-xs text-gray-500 shrink-0">
                    {timeAgo(note.updated_at, tCommon)}
                  </span>
                </div>
                <p className="text-xs text-gray-500 truncate mt-0.5">{note.content.slice(0, 60)}</p>
                {note.tags.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-1">
                    {note.tags.slice(0, 3).map(tag => (
                      <span
                        key={tag}
                        className="px-1.5 py-0.5 bg-gray-700 text-gray-400 text-xs rounded"
                      >
                        {tag}
                      </span>
                    ))}
                    {note.tags.length > 3 && (
                      <span className="px-1.5 py-0.5 bg-gray-700 text-gray-400 text-xs rounded">
                        {t('moreTagsCount', { count: note.tags.length - 3 })}
                      </span>
                    )}
                  </div>
                )}
              </button>
            ))
          )}
        </div>
      </aside>

      {/* Right panel — editor / viewer */}
      <main className="flex-1 min-w-0 flex flex-col bg-gray-900">
        {isCreating || selectedNote ? (
          <NoteEditor
            key={selectedNote?.id ?? 'new'}
            ref={editorRef}
            note={selectedNote}
            isCreating={isCreating}
            error={error}
            onSave={handleSave}
            onDelete={note => setDeleteTarget(note)}
            onClose={closeEditor}
            onDirtyChange={setIsDirty}
          />
        ) : (
          <div className="flex-1 flex flex-col items-center justify-center text-center p-8">
            <FileText size={48} className="text-gray-700 mb-4" />
            <h2 className="text-xl font-semibold text-gray-400 mb-2">{t('selectNote.heading')}</h2>
            <p className="text-gray-600 text-sm mb-4">
              {t('selectNote.description')}
            </p>
            <button
              onClick={startCreating}
              className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm transition-colors cursor-pointer"
            >
              <Plus size={16} />
              {t('newNote')}
            </button>
          </div>
        )}
      </main>
    </div>
    </>
  )
}
