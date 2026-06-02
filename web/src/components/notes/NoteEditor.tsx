import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { Tag, Trash2, Save, Eye, Edit3, X, Check, Loader2, CloudOff } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNow } from '../../hooks/useNow'
import type { Note, NoteInput, ViewMode } from '../../hooks/useNotes'

/** Delay after the last keystroke before an existing note autosaves. */
const AUTOSAVE_DELAY_MS = 1500

/** Lifecycle of the most recent save attempt, surfaced in the status label. */
type SaveState = 'idle' | 'saving' | 'saved' | 'error'

interface NoteEditorProps {
  /** The note being edited, or null when creating a new note. */
  note: Note | null
  /** True when in create mode (no existing note yet). */
  isCreating: boolean
  /** Error message to surface in the toolbar (from the data layer). */
  error: string
  /** Persist the draft. Returns the saved note, or null on failure. */
  onSave: (input: NoteInput) => Promise<Note | null>
  /** Request deletion of the given note (the page owns the confirm dialog). */
  onDelete: (note: Note) => void
  /** Close the editor without saving. */
  onClose: () => void
  /**
   * Report whether the draft currently has unsaved changes, so the parent can
   * guard navigation away from the editor with a discard confirmation.
   */
  onDirtyChange: (dirty: boolean) => void
}

/** Imperative handle so the page can trigger a save from a keyboard shortcut. */
export interface NoteEditorHandle {
  /** Persist the current draft if there are unsaved changes and no save is in flight. */
  save: () => void
}

function parseTags(raw: string): string[] {
  return raw
    .split(',')
    .map(t => t.trim())
    .filter(t => t.length > 0)
}

/**
 * Small label near the toolbar reflecting the autosave/save lifecycle:
 * "Saving…" while a request is in flight, "Saved <relative time> ago" after a
 * success (refreshing once a second), and "Offline" when a save fails. Renders
 * nothing while idle; mounting only after the first save keeps the per-second
 * tick from {@link useNow} off the hot editing path.
 */
function SaveStatus({ state, lastSavedAt }: { state: SaveState; lastSavedAt: number | null }) {
  const { t } = useTranslation('notes')
  const now = useNow()

  if (state === 'saving') {
    return (
      <span className="flex items-center gap-1.5 text-xs text-gray-400" role="status" aria-live="polite">
        <Loader2 size={12} className="animate-spin" />
        {t('status.saving')}
      </span>
    )
  }
  if (state === 'error') {
    return (
      <span className="flex items-center gap-1.5 text-xs text-amber-400" role="status" aria-live="polite">
        <CloudOff size={12} />
        {t('status.offline')}
      </span>
    )
  }
  if (state === 'saved' && lastSavedAt !== null) {
    const secs = Math.max(0, Math.floor((now.getTime() - lastSavedAt) / 1000))
    let label: string
    if (secs < 5) label = t('status.savedJustNow')
    else if (secs < 60) label = t('status.savedSecondsAgo', { count: secs })
    else if (secs < 3600) label = t('status.savedMinutesAgo', { count: Math.floor(secs / 60) })
    else label = t('status.savedHoursAgo', { count: Math.floor(secs / 3600) })
    return (
      <span className="flex items-center gap-1.5 text-xs text-gray-500" role="status" aria-live="polite">
        <Check size={12} />
        {label}
      </span>
    )
  }
  return null
}

/**
 * Editor pane for a single note. Owns draft state, edit/preview toggling, and
 * the markdown rendering. The parent re-mounts this (via a `key`) when the
 * active note changes so drafts initialize cleanly from props.
 */
const NoteEditor = forwardRef<NoteEditorHandle, NoteEditorProps>(function NoteEditor(
  { note, isCreating, error, onSave, onDelete, onClose, onDirtyChange },
  ref,
) {
  const { t } = useTranslation('notes')
  const [draftTitle, setDraftTitle] = useState(note?.title ?? '')
  const [draftContent, setDraftContent] = useState(note?.content ?? '')
  const [draftTags, setDraftTags] = useState(note ? note.tags.join(', ') : '')
  const [viewMode, setViewMode] = useState<ViewMode>('edit')
  const [saving, setSaving] = useState(false)
  const [saveState, setSaveState] = useState<SaveState>('idle')
  const [lastSavedAt, setLastSavedAt] = useState<number | null>(null)

  const hasChanges = note
    ? draftTitle !== note.title ||
      draftContent !== note.content ||
      draftTags !== note.tags.join(', ')
    : isCreating

  useEffect(() => {
    onDirtyChange(hasChanges)
    return () => onDirtyChange(false)
  }, [hasChanges, onDirtyChange])

  async function handleSave(input?: NoteInput) {
    setSaving(true)
    setSaveState('saving')
    try {
      const payload = input ?? {
        id: note?.id,
        title: draftTitle,
        content: draftContent,
        tags: parseTags(draftTags),
      }
      const saved = await onSave(payload)
      if (saved) {
        setDraftTitle(saved.title)
        setDraftContent(saved.content)
        setDraftTags(saved.tags.join(', '))
        setSaveState('saved')
        setLastSavedAt(Date.now())
      } else {
        // onSave swallows the failure and surfaces a message via `error`;
        // mark the status as offline/errored so the indicator reflects it.
        setSaveState('error')
      }
    } finally {
      setSaving(false)
    }
  }

  // Expose save() so the page-level Cmd/Ctrl+S shortcut can persist the draft.
  // Mirror the Save button's disabled state so the shortcut is a no-op when
  // there is nothing to save or a save is already in flight. The handle is
  // kept stable but defers to a ref that is refreshed every render, so the
  // shortcut always saves the *current* draft rather than a stale closure
  // captured the first time `hasChanges`/`saving` changed.
  const saveRef = useRef(() => {})
  useEffect(() => {
    saveRef.current = () => {
      if (!saving && hasChanges) handleSave()
    }
  })
  useImperativeHandle(ref, () => ({
    save() {
      saveRef.current()
    },
  }), [])

  // Debounced autosave for existing notes. The latest save logic lives on a ref
  // so the effect can depend only on the draft/guard values (resetting the
  // timer on each keystroke) without re-subscribing to a fresh `handleSave`
  // closure every render.
  const autosaveRef = useRef((_input: NoteInput) => {})
  useEffect(() => {
    autosaveRef.current = (input: NoteInput) => {
      if (note && input.id === note.id && !isCreating && hasChanges && !saving && saveState !== 'error') handleSave(input)
    }
  })
  useEffect(() => {
    // New-note creation stays on the manual Save button; never autosave it.
    // Also hold off while a save is in flight to avoid request pileups.
    if (!note || isCreating || !hasChanges || saving || saveState === 'error') return
    const input: NoteInput = {
      id: note.id,
      title: draftTitle,
      content: draftContent,
      tags: parseTags(draftTags),
    }
    const timer = setTimeout(() => autosaveRef.current(input), AUTOSAVE_DELAY_MS)
    // Clearing on every dependency change debounces rapid typing into a single
    // request; the same cleanup runs on unmount and when the parent re-mounts
    // this editor for a different note, so no stray PUT targets the old note.
    return () => clearTimeout(timer)
  }, [draftTitle, draftContent, draftTags, note, isCreating, hasChanges, saving, saveState])

  return (
    <>
      {/* Toolbar */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-gray-800 shrink-0">
        <div className="flex rounded overflow-hidden border border-gray-700">
          <button
            onClick={() => setViewMode('edit')}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-sm transition-colors cursor-pointer ${
              viewMode === 'edit'
                ? 'bg-gray-700 text-white'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            <Edit3 size={14} />
            {t('editor.edit')}
          </button>
          <button
            onClick={() => setViewMode('preview')}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-sm transition-colors cursor-pointer ${
              viewMode === 'preview'
                ? 'bg-gray-700 text-white'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            <Eye size={14} />
            {t('editor.preview')}
          </button>
        </div>

        <div className="ml-auto flex items-center gap-2">
          {saveState !== 'idle' && <SaveStatus state={saveState} lastSavedAt={lastSavedAt} />}
          {error && <span className="text-red-400 text-sm">{error}</span>}
          <button
            onClick={() => handleSave()}
            disabled={saving || !hasChanges}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 disabled:cursor-default text-white rounded text-sm transition-colors cursor-pointer"
            title={t('shortcuts.save')}
          >
            <Save size={14} />
            {saving ? t('editor.saving') : t('editor.save')}
          </button>
          {note && (
            <button
              onClick={() => onDelete(note)}
              className="flex items-center gap-1.5 px-3 py-1.5 text-red-400 hover:text-red-300 hover:bg-gray-800 rounded text-sm transition-colors cursor-pointer"
              title={t('editor.deleteNote')}
              aria-label={t('editor.deleteNote')}
            >
              <Trash2 size={14} />
            </button>
          )}
          <button
            onClick={onClose}
            className="flex items-center gap-1 px-2 py-1.5 text-gray-400 hover:text-white hover:bg-gray-800 rounded text-sm transition-colors cursor-pointer"
            title={t('editor.closeLabel')}
            aria-label={t('editor.closeLabel')}
          >
            <X size={16} />
          </button>
        </div>
      </div>

      {/* Note meta: title + tags */}
      {viewMode === 'edit' && (
        <div className="px-6 pt-4 space-y-2 shrink-0">
          <input
            type="text"
            placeholder={t('fields.titlePlaceholder')}
            value={draftTitle}
            onChange={e => setDraftTitle(e.target.value)}
            className="w-full bg-transparent text-2xl font-bold text-white placeholder-gray-600 focus:outline-none"
            aria-label={t('fields.titleLabel')}
          />
          <div className="flex items-center gap-2">
            <Tag size={14} className="text-gray-500 shrink-0" />
            <input
              type="text"
              placeholder={t('fields.tagsPlaceholder')}
              value={draftTags}
              onChange={e => setDraftTags(e.target.value)}
              className="flex-1 bg-transparent text-sm text-gray-400 placeholder-gray-600 focus:outline-none"
              aria-label={t('fields.tagsLabel')}
            />
          </div>
          <hr className="border-gray-800" />
        </div>
      )}

      {viewMode === 'edit' ? (
        <textarea
          value={draftContent}
          onChange={e => setDraftContent(e.target.value)}
          placeholder={t('fields.contentPlaceholder')}
          className="flex-1 px-6 py-4 bg-transparent text-gray-200 text-sm font-mono leading-relaxed resize-none focus:outline-none placeholder-gray-600"
          aria-label={t('fields.contentLabel')}
          spellCheck
        />
      ) : (
        <div className="flex-1 overflow-y-auto px-6 py-4">
          <h1 className="text-2xl font-bold text-white mb-1">
            {draftTitle || <span className="text-gray-500 italic">{t('untitled')}</span>}
          </h1>
          {draftTags && (
            <div className="flex flex-wrap gap-1 mb-4">
              {parseTags(draftTags).map((tag, i) => (
                <span
                  key={`${i}-${tag}`}
                  className="px-2 py-0.5 bg-gray-700 text-gray-300 text-xs rounded"
                >
                  {tag}
                </span>
              ))}
            </div>
          )}
          <div className="prose prose-invert prose-sm max-w-none">
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              components={{
                code({ className, children, ...props }) {
                  const match = /language-(\S+)/.exec(className ?? '')
                  const isBlock = !!match
                  return isBlock ? (
                    <SyntaxHighlighter
                      style={vscDarkPlus}
                      language={match![1]}
                      PreTag="div"
                    >
                      {String(children).replace(/\n$/, '')}
                    </SyntaxHighlighter>
                  ) : (
                    <code
                      className="px-1 py-0.5 bg-gray-800 rounded text-sm font-mono text-gray-200"
                      {...props}
                    >
                      {children}
                    </code>
                  )
                },
                h1: ({ children }) => (
                  <h1 className="text-2xl font-bold text-white mt-6 mb-3">{children}</h1>
                ),
                h2: ({ children }) => (
                  <h2 className="text-xl font-semibold text-white mt-5 mb-2">{children}</h2>
                ),
                h3: ({ children }) => (
                  <h3 className="text-lg font-semibold text-white mt-4 mb-2">{children}</h3>
                ),
                p: ({ children }) => (
                  <p className="text-gray-300 mb-3 leading-relaxed">{children}</p>
                ),
                ul: ({ children }) => (
                  <ul className="list-disc list-inside text-gray-300 mb-3 space-y-1">
                    {children}
                  </ul>
                ),
                ol: ({ children }) => (
                  <ol className="list-decimal list-inside text-gray-300 mb-3 space-y-1">
                    {children}
                  </ol>
                ),
                li: ({ children }) => <li className="text-gray-300">{children}</li>,
                blockquote: ({ children }) => (
                  <blockquote className="border-l-4 border-gray-600 pl-4 text-gray-400 italic mb-3">
                    {children}
                  </blockquote>
                ),
                a: ({ href, children }) => (
                  <a
                    href={href}
                    className="text-blue-400 hover:text-blue-300 underline"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {children}
                  </a>
                ),
                strong: ({ children }) => (
                  <strong className="font-semibold text-white">{children}</strong>
                ),
                em: ({ children }) => <em className="italic text-gray-200">{children}</em>,
                hr: () => <hr className="border-gray-700 my-4" />,
                table: ({ children }) => (
                  <div className="overflow-x-auto mb-3">
                    <table className="w-full text-sm text-gray-300 border-collapse">
                      {children}
                    </table>
                  </div>
                ),
                th: ({ children }) => (
                  <th className="border border-gray-700 px-3 py-1.5 bg-gray-800 font-semibold text-white text-left">
                    {children}
                  </th>
                ),
                td: ({ children }) => (
                  <td className="border border-gray-700 px-3 py-1.5">{children}</td>
                ),
              }}
            >
              {draftContent || t('fields.nothingToPreview')}
            </ReactMarkdown>
          </div>
        </div>
      )}
    </>
  )
})

export default NoteEditor
