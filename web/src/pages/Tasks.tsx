import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Plus,
  X,
  ChevronDown,
  ChevronUp,
  Archive,
  ArchiveRestore,
} from 'lucide-react'

interface Task {
  id: number
  user_id: number
  title: string
  body: string
  archived: boolean
  created_at: string
  updated_at: string
  archived_at?: string | null
  tags: string[]
  note_count: number
}

interface TaskNote {
  id: number
  task_id: number
  content: string
  created_at: string
}

const BUILT_IN_TAGS = ['work', 'personal'] as const

function formatRelative(iso: string, language: string, justNow: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const now = Date.now()
  const diffSec = Math.round((then - now) / 1000)
  const abs = Math.abs(diffSec)
  const rtf = new Intl.RelativeTimeFormat(language, { numeric: 'auto' })
  if (abs < 30) return justNow
  if (abs < 60) return rtf.format(diffSec, 'second')
  if (abs < 60 * 60) return rtf.format(Math.round(diffSec / 60), 'minute')
  if (abs < 60 * 60 * 24) return rtf.format(Math.round(diffSec / 3600), 'hour')
  if (abs < 60 * 60 * 24 * 7) return rtf.format(Math.round(diffSec / 86400), 'day')
  if (abs < 60 * 60 * 24 * 30) return rtf.format(Math.round(diffSec / (86400 * 7)), 'week')
  if (abs < 60 * 60 * 24 * 365) return rtf.format(Math.round(diffSec / (86400 * 30)), 'month')
  return rtf.format(Math.round(diffSec / (86400 * 365)), 'year')
}

function formatAbsolute(iso: string, language: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return new Intl.DateTimeFormat(language, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(d)
}

export default function Tasks() {
  const { t, i18n } = useTranslation('tasks')

  const [tasks, setTasks] = useState<Task[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showArchived, setShowArchived] = useState(false)
  const [activeTagFilter, setActiveTagFilter] = useState<string | null>(null)
  const [expandedId, setExpandedId] = useState<number | null>(null)

  // Composer state
  const [newTaskTitle, setNewTaskTitle] = useState('')
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [customTagInput, setCustomTagInput] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // Expanded-card cache: task ID -> notes loaded from API.
  const [notesById, setNotesById] = useState<Record<number, TaskNote[]>>({})
  const [notesLoadingId, setNotesLoadingId] = useState<number | null>(null)
  const [noteDrafts, setNoteDrafts] = useState<Record<number, string>>({})
  const [bodyDrafts, setBodyDrafts] = useState<Record<number, string>>({})

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      try {
        const res = await fetch(`/api/tasks?archived=${showArchived ? 'true' : 'false'}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('errors.failedToLoad'))
        const data: { tasks?: Task[] } = await res.json()
        setTasks(data.tasks ?? [])
        setError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [showArchived, t])

  const allTags = useMemo(() => {
    const set = new Set<string>()
    tasks.forEach(task => task.tags.forEach(tag => set.add(tag)))
    return Array.from(set).sort()
  }, [tasks])

  const visibleTasks = useMemo(() => {
    if (!activeTagFilter) return tasks
    return tasks.filter(task => task.tags.includes(activeTagFilter))
  }, [tasks, activeTagFilter])

  function toggleTag(tag: string) {
    setSelectedTags(prev => prev.includes(tag) ? prev.filter(x => x !== tag) : [...prev, tag])
  }

  function handleCustomTagKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault()
      const value = customTagInput.trim()
      if (value && !selectedTags.includes(value)) {
        setSelectedTags(prev => [...prev, value])
      }
      setCustomTagInput('')
    }
  }

  async function submitNewTask(e: React.FormEvent) {
    e.preventDefault()
    const title = newTaskTitle.trim()
    if (!title || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const res = await fetch('/api/tasks', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title, tags: selectedTags }),
      })
      if (!res.ok) {
        let msg = t('errors.failedToCreate')
        try { const data = await res.json(); msg = data.error ?? msg } catch { /* non-JSON body */ }
        throw new Error(msg)
      }
      const data: { task: Task } = await res.json()
      if (!showArchived) {
        setTasks(prev => [data.task, ...prev])
      }
      setNewTaskTitle('')
      setSelectedTags([])
      setCustomTagInput('')
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToCreate'))
    } finally {
      setSubmitting(false)
    }
  }

  async function toggleExpanded(task: Task) {
    if (expandedId === task.id) {
      setExpandedId(null)
      return
    }
    setExpandedId(task.id)
    setBodyDrafts(prev => prev[task.id] !== undefined ? prev : { ...prev, [task.id]: task.body })
    if (notesById[task.id]) return
    setError('')
    setNotesLoadingId(task.id)
    try {
      const res = await fetch(`/api/tasks/${task.id}/notes`, { credentials: 'include' })
      if (!res.ok) throw new Error(t('errors.failedToLoadNotes'))
      const data: { notes?: TaskNote[] } = await res.json()
      setNotesById(prev => ({ ...prev, [task.id]: data.notes ?? [] }))
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToLoadNotes'))
    } finally {
      setNotesLoadingId(null)
    }
  }

  async function saveBody(task: Task) {
    const draft = bodyDrafts[task.id] ?? ''
    if (draft === task.body) return
    setError('')
    try {
      const res = await fetch(`/api/tasks/${task.id}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: draft }),
      })
      if (!res.ok) throw new Error(t('errors.failedToUpdate'))
      const data: { task: Task } = await res.json()
      setTasks(prev => prev.map(existing => existing.id === data.task.id ? data.task : existing))
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToUpdate'))
    }
  }

  async function toggleArchive(task: Task) {
    const nextArchived = !task.archived
    setError('')
    try {
      const res = await fetch(`/api/tasks/${task.id}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ archived: nextArchived }),
      })
      if (!res.ok) throw new Error(t('errors.failedToUpdate'))
      const data: { task: Task } = await res.json()
      // Remove from view if it no longer matches the showArchived filter.
      setTasks(prev => {
        if (data.task.archived === showArchived) {
          return prev.map(existing => existing.id === data.task.id ? data.task : existing)
        }
        return prev.filter(existing => existing.id !== data.task.id)
      })
      if (data.task.archived !== showArchived) {
        setExpandedId(prev => prev === data.task.id ? null : prev)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToUpdate'))
    }
  }

  async function addNote(task: Task) {
    const content = (noteDrafts[task.id] ?? '').trim()
    if (!content) return
    setError('')
    try {
      const res = await fetch(`/api/tasks/${task.id}/notes`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content }),
      })
      if (!res.ok) throw new Error(t('errors.failedToAddNote'))
      const data: { note: TaskNote } = await res.json()
      setNotesById(prev => ({
        ...prev,
        [task.id]: [...(prev[task.id] ?? []), data.note],
      }))
      setNoteDrafts(prev => ({ ...prev, [task.id]: '' }))
      setTasks(prev => prev.map(existing =>
        existing.id === task.id ? { ...existing, note_count: existing.note_count + 1 } : existing,
      ))
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToAddNote'))
    }
  }

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <div className="max-w-3xl mx-auto px-4 py-6 space-y-4">
        <header className="flex items-center justify-between gap-2">
          <h1 className="text-2xl font-semibold">{t('pageTitle')}</h1>
          <label className="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
            <input
              type="checkbox"
              checked={showArchived}
              onChange={e => {
                setShowArchived(e.target.checked)
                setExpandedId(null)
              }}
              className="rounded border-gray-700 bg-gray-800"
              aria-label={t('showArchived')}
            />
            <span>{t('showArchived')}</span>
          </label>
        </header>

        {error && (
          <div role="alert" className="px-3 py-2 bg-red-900/40 border border-red-800 text-red-300 text-sm rounded">
            {error}
          </div>
        )}

        {/* Composer */}
        <form onSubmit={submitNewTask} className="space-y-2 p-3 bg-gray-800/40 border border-gray-800 rounded-lg">
          <div className="flex gap-2">
            <input
              type="text"
              value={newTaskTitle}
              onChange={e => setNewTaskTitle(e.target.value)}
              placeholder={t('inputPlaceholder')}
              aria-label={t('inputPlaceholder')}
              className="flex-1 min-w-0 px-3 py-2 bg-gray-900 border border-gray-700 rounded text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
            />
            <button
              type="submit"
              disabled={submitting || !newTaskTitle.trim()}
              className="flex items-center gap-1 px-3 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 disabled:cursor-default text-white rounded text-sm transition-colors cursor-pointer shrink-0"
              aria-label={t('addTask')}
            >
              <Plus size={16} />
              <span className="hidden sm:inline">{t('addTask')}</span>
            </button>
          </div>

          <div className="flex flex-wrap gap-1.5 items-center">
            <span className="text-xs text-gray-500 mr-1">{t('tags.label')}:</span>
            {BUILT_IN_TAGS.map(tag => (
              <button
                key={tag}
                type="button"
                onClick={() => toggleTag(tag)}
                className={`px-2 py-0.5 rounded-full text-xs transition-colors cursor-pointer ${
                  selectedTags.includes(tag)
                    ? 'bg-blue-600 text-white'
                    : 'bg-gray-800 text-gray-400 hover:text-white'
                }`}
              >
                {tag === 'work' ? t('tags.work') : t('tags.personal')}
              </button>
            ))}
            {selectedTags.filter(tag => !(BUILT_IN_TAGS as readonly string[]).includes(tag)).map(tag => (
              <span
                key={tag}
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs bg-blue-600 text-white"
              >
                {tag}
                <button
                  type="button"
                  onClick={() => toggleTag(tag)}
                  className="hover:text-gray-200 cursor-pointer"
                  aria-label={t('tags.removeTag', { tag })}
                >
                  <X size={10} />
                </button>
              </span>
            ))}
            <input
              type="text"
              value={customTagInput}
              onChange={e => setCustomTagInput(e.target.value)}
              onKeyDown={handleCustomTagKeyDown}
              placeholder={t('tags.customPlaceholder')}
              aria-label={t('tags.addCustom')}
              className="flex-1 min-w-[120px] px-2 py-0.5 bg-transparent border border-gray-700 rounded-full text-xs text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
            />
          </div>
        </form>

        {/* Tag filter chips */}
        {allTags.length > 0 && (
          <div
            className="flex flex-wrap gap-1.5"
            role="group"
            aria-label={t('filter.label')}
          >
            {allTags.map(tag => (
              <button
                key={tag}
                type="button"
                onClick={() => setActiveTagFilter(activeTagFilter === tag ? null : tag)}
                className={`px-2 py-0.5 rounded-full text-xs transition-colors cursor-pointer ${
                  activeTagFilter === tag
                    ? 'bg-blue-600 text-white'
                    : 'bg-gray-800 text-gray-400 hover:text-white'
                }`}
                aria-pressed={activeTagFilter === tag}
              >
                {tag}
              </button>
            ))}
            {activeTagFilter && (
              <button
                type="button"
                onClick={() => setActiveTagFilter(null)}
                className="px-2 py-0.5 rounded-full text-xs bg-gray-800 text-gray-400 hover:text-white transition-colors cursor-pointer"
                aria-label={t('filter.clearLabel')}
              >
                <X size={12} />
              </button>
            )}
          </div>
        )}

        {/* Task list */}
        <ul className="space-y-2" aria-busy={loading}>
          {!loading && visibleTasks.length === 0 && (
            <li className="px-3 py-6 text-center text-gray-500 text-sm">
              {activeTagFilter
                ? t('empty.filtered')
                : showArchived
                  ? t('empty.archived')
                  : t('empty.active')}
            </li>
          )}
          {visibleTasks.map(task => {
            const isExpanded = expandedId === task.id
            const cardDim = task.archived ? 'opacity-60' : ''
            return (
              <li
                key={task.id}
                className={`bg-gray-800/40 border border-gray-800 rounded-lg ${cardDim}`}
                data-testid={`task-card-${task.id}`}
                data-archived={task.archived ? 'true' : 'false'}
              >
                <button
                  type="button"
                  onClick={() => toggleExpanded(task)}
                  aria-expanded={isExpanded}
                  aria-label={isExpanded ? t('collapse') : t('expand')}
                  className="w-full text-left px-3 py-2.5 flex items-start gap-2 hover:bg-gray-800/60 transition-colors cursor-pointer"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <p className="text-sm font-medium text-white">{task.title}</p>
                      {task.archived && (
                        <span className="px-1.5 py-0.5 rounded text-[10px] uppercase tracking-wide bg-gray-700 text-gray-400">
                          {t('archivedLabel')}
                        </span>
                      )}
                    </div>
                    <div className="flex flex-wrap gap-1.5 mt-1 items-center">
                      {task.tags.map(tag => (
                        <span
                          key={tag}
                          className="px-1.5 py-0.5 bg-gray-700 text-gray-300 text-[10px] rounded-full"
                        >
                          {tag}
                        </span>
                      ))}
                      {task.note_count > 0 && (
                        <span className="text-[10px] text-gray-400">
                          {t('notes.badge', { count: task.note_count })}
                        </span>
                      )}
                    </div>
                    <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-gray-500">
                      <span title={formatAbsolute(task.created_at, i18n.language)}>
                        {t('time.created', { relative: formatRelative(task.created_at, i18n.language, t('time.justNow')) })}
                      </span>
                      <span title={formatAbsolute(task.updated_at, i18n.language)}>
                        {t('time.updated', { relative: formatRelative(task.updated_at, i18n.language, t('time.justNow')) })}
                      </span>
                    </div>
                  </div>
                  <span className="text-gray-400 shrink-0 mt-1">
                    {isExpanded ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
                  </span>
                </button>

                {isExpanded && (
                  <div className="px-3 pb-3 pt-1 border-t border-gray-800 space-y-3">
                    <div>
                      <label className="block text-xs text-gray-400 mb-1" htmlFor={`task-body-${task.id}`}>
                        {t('body.label')}
                      </label>
                      <textarea
                        id={`task-body-${task.id}`}
                        rows={3}
                        value={bodyDrafts[task.id] ?? task.body}
                        onChange={e => setBodyDrafts(prev => ({ ...prev, [task.id]: e.target.value }))}
                        onBlur={() => saveBody(task)}
                        placeholder={t('body.placeholder')}
                        className="w-full px-2 py-1.5 bg-gray-900 border border-gray-700 rounded text-sm text-gray-200 placeholder-gray-600 focus:outline-none focus:border-blue-500 resize-y"
                      />
                    </div>

                    <div>
                      <h3 className="text-xs text-gray-400 mb-1">{t('notes.heading')}</h3>
                      <ul className="space-y-1.5">
                        {notesLoadingId === task.id && !notesById[task.id] && (
                          <li className="text-xs text-gray-500">…</li>
                        )}
                        {(notesById[task.id] ?? []).length === 0 && notesLoadingId !== task.id && (
                          <li className="text-xs text-gray-500">{t('notes.empty')}</li>
                        )}
                        {(notesById[task.id] ?? []).map(note => (
                          <li key={note.id} className="px-2 py-1.5 bg-gray-900/60 border border-gray-800 rounded text-xs text-gray-200">
                            <p className="whitespace-pre-wrap break-words">{note.content}</p>
                            <p className="text-[10px] text-gray-500 mt-0.5" title={formatAbsolute(note.created_at, i18n.language)}>
                              {formatRelative(note.created_at, i18n.language, t('time.justNow'))}
                            </p>
                          </li>
                        ))}
                      </ul>

                      <div className="mt-2 flex gap-2">
                        <input
                          type="text"
                          value={noteDrafts[task.id] ?? ''}
                          onChange={e => setNoteDrafts(prev => ({ ...prev, [task.id]: e.target.value }))}
                          onKeyDown={e => {
                            if (e.key === 'Enter') {
                              e.preventDefault()
                              addNote(task)
                            }
                          }}
                          placeholder={t('notes.composerPlaceholder')}
                          aria-label={t('notes.composerPlaceholder')}
                          className="flex-1 min-w-0 px-2 py-1.5 bg-gray-900 border border-gray-700 rounded text-xs text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                        />
                        <button
                          type="button"
                          onClick={() => addNote(task)}
                          disabled={!(noteDrafts[task.id] ?? '').trim()}
                          className="px-2 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 disabled:cursor-default text-white rounded text-xs transition-colors cursor-pointer"
                        >
                          {t('notes.add')}
                        </button>
                      </div>
                    </div>

                    <div className="flex justify-end">
                      <button
                        type="button"
                        onClick={() => toggleArchive(task)}
                        className="flex items-center gap-1 px-2 py-1 text-xs text-gray-400 hover:text-white hover:bg-gray-800 rounded transition-colors cursor-pointer"
                      >
                        {task.archived ? <ArchiveRestore size={14} /> : <Archive size={14} />}
                        <span>{task.archived ? t('unarchive') : t('archive')}</span>
                      </button>
                    </div>
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      </div>
    </div>
  )
}
