import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Archive,
  ChevronDown,
  ChevronRight,
  Dumbbell,
  Loader2,
  Pin,
  Send,
  Sparkles,
  Star,
} from 'lucide-react'
import type { StrideWorkout } from '../../types/stride'
import { Dialog, DialogBody, DialogHeader } from '../ui/dialog'

// WorkoutLibrary is the Stride page's library section: the reusable sessions
// the weekly coach rotates through. One entry is pinned as the weekly
// reference benchmark; the rest carry ratings, block membership and usage
// stats. New entries are designed conversationally in the AI chat dialog —
// no manual punching required (though rows stay editable via rating/pin/
// archive actions here).

const BLOCK_CLASSES: Record<string, string> = {
  base: 'bg-sky-500/10 text-sky-300',
  build: 'bg-orange-500/10 text-orange-300',
  peak: 'bg-red-500/10 text-red-300',
  taper: 'bg-green-500/10 text-green-300',
}

const TYPE_CLASSES: Record<string, string> = {
  threshold: 'bg-yellow-500/10 text-yellow-300',
  hard: 'bg-red-500/10 text-red-300',
  easy: 'bg-green-500/10 text-green-300',
  long_run: 'bg-sky-500/10 text-sky-300',
  strides: 'bg-purple-500/10 text-purple-300',
}

interface ChatMessage {
  role: 'user' | 'coach'
  content: string
}

export default function WorkoutLibrary() {
  const { t } = useTranslation('stride')
  const [workouts, setWorkouts] = useState<StrideWorkout[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [chatOpen, setChatOpen] = useState(false)

  const reload = useCallback(async () => {
    try {
      const res = await fetch('/api/stride/workouts', { credentials: 'include' })
      if (!res.ok) {
        setError(t('library.loadError'))
        return
      }
      const data = await res.json()
      setWorkouts(data.workouts ?? [])
      setError('')
    } catch {
      setError(t('library.loadError'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial data fetch; reload's setState calls all happen after the await resolves
    void reload()
  }, [reload])

  // Patch one workout via PUT with the full row (the API replaces fields).
  const patchWorkout = useCallback(
    async (w: StrideWorkout, changes: Partial<StrideWorkout>) => {
      const payload = { ...w, ...changes }
      try {
        const res = await fetch(`/api/stride/workouts/${w.id}`, {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        })
        if (res.ok) void reload()
      } catch {
        // Row keeps its previous state; next reload reconciles.
      }
    },
    [reload],
  )

  return (
    <section>
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <Dumbbell size={18} className="text-yellow-400" />
          <h2 className="text-lg font-semibold text-white">{t('library.title')}</h2>
          {workouts.length > 0 && (
            <span className="rounded-full bg-gray-800 px-2 py-0.5 text-xs text-gray-400">
              {workouts.length}
            </span>
          )}
        </div>
        <button
          type="button"
          onClick={() => setChatOpen(true)}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-purple-600 hover:bg-purple-700 text-white rounded-lg transition-colors"
        >
          <Sparkles size={14} />
          {t('library.designWithAi')}
        </button>
      </div>

      {error && <p className="text-sm text-red-400 mb-3">{error}</p>}
      {loading ? (
        <div className="animate-pulse space-y-2">
          <div className="h-14 bg-gray-800 rounded-xl" />
          <div className="h-14 bg-gray-800 rounded-xl" />
        </div>
      ) : (
        <ul className="space-y-2">
          {workouts.map((w) => {
            const expanded = expandedId === w.id
            return (
              <li key={w.id} className="bg-gray-800 border border-gray-700 rounded-xl">
                <div className="flex items-start gap-2 p-3">
                  <button
                    type="button"
                    onClick={() => setExpandedId(expanded ? null : w.id)}
                    aria-expanded={expanded}
                    className="mt-0.5 text-gray-400 hover:text-gray-200"
                    aria-label={expanded ? t('library.collapse') : t('library.expand')}
                  >
                    {expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                  </button>
                  <div className="flex-1 min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium text-white truncate">{w.name}</span>
                      {w.is_reference && (
                        <span
                          className="inline-flex items-center gap-1 rounded-full bg-yellow-500/10 px-2 py-0.5 text-[10px] text-yellow-300"
                          title={t('library.referenceHint')}
                        >
                          <Pin size={10} />
                          {t('library.reference')}
                        </span>
                      )}
                      {w.workout_type && (
                        <span className={`rounded-full px-2 py-0.5 text-[10px] ${TYPE_CLASSES[w.workout_type] ?? 'bg-gray-700 text-gray-300'}`}>
                          {t(`library.types.${w.workout_type}`, { defaultValue: w.workout_type })}
                        </span>
                      )}
                      {w.blocks.map((b) => (
                        <span key={b} className={`rounded-full px-2 py-0.5 text-[10px] ${BLOCK_CLASSES[b] ?? 'bg-gray-700 text-gray-300'}`}>
                          {t(`library.blocks.${b}`, { defaultValue: b })}
                        </span>
                      ))}
                    </div>
                    <p className="mt-1 text-xs text-gray-400 truncate">{w.main_set}</p>
                    <div className="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500">
                      <span className="inline-flex items-center gap-0.5">
                        {[1, 2, 3, 4, 5].map((n) => (
                          <button
                            key={n}
                            type="button"
                            onClick={() => void patchWorkout(w, { rating: w.rating === n ? 0 : n })}
                            aria-label={t('library.rate', { n })}
                            className="text-gray-600 hover:text-yellow-300"
                          >
                            <Star
                              size={12}
                              className={n <= w.rating ? 'fill-yellow-400 text-yellow-400' : ''}
                            />
                          </button>
                        ))}
                      </span>
                      <span>{t('library.usedCount', { count: w.times_used })}</span>
                      {w.last_used_at && (
                        <span>{t('library.lastUsed', { week: w.last_used_at })}</span>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-1">
                    {!w.is_reference && (
                      <button
                        type="button"
                        onClick={() => void patchWorkout(w, { is_reference: true })}
                        title={t('library.makeReference')}
                        aria-label={t('library.makeReference')}
                        className="p-1.5 rounded-md text-gray-500 hover:text-yellow-300 hover:bg-gray-700"
                      >
                        <Pin size={14} />
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => void patchWorkout(w, { archived: true })}
                      title={t('library.archive')}
                      aria-label={t('library.archive')}
                      className="p-1.5 rounded-md text-gray-500 hover:text-red-300 hover:bg-gray-700"
                    >
                      <Archive size={14} />
                    </button>
                  </div>
                </div>
                {expanded && (
                  <div className="border-t border-gray-700/60 px-4 py-3 text-sm text-gray-300 space-y-1">
                    {w.warmup && (
                      <p><span className="text-gray-500">{t('library.fields.warmup')}: </span>{w.warmup}</p>
                    )}
                    <p><span className="text-gray-500">{t('library.fields.mainSet')}: </span>{w.main_set}</p>
                    {w.cooldown && (
                      <p><span className="text-gray-500">{t('library.fields.cooldown')}: </span>{w.cooldown}</p>
                    )}
                    {w.strides && (
                      <p><span className="text-gray-500">{t('library.fields.strides')}: </span>{w.strides}</p>
                    )}
                    {w.target_hr_cap && (
                      <p><span className="text-gray-500">{t('library.fields.hrCap')}: </span>{w.target_hr_cap}</p>
                    )}
                    {w.description && <p className="text-gray-400 text-xs pt-1">{w.description}</p>}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}

      <WorkoutChatDialog
        open={chatOpen}
        onClose={() => setChatOpen(false)}
        onSaved={() => {
          setChatOpen(false)
          void reload()
        }}
      />
    </section>
  )
}

interface WorkoutChatDialogProps {
  open: boolean
  onClose: () => void
  onSaved: () => void
}

// WorkoutChatDialog is the conversational designer: describe the session you
// want, iterate on the coach's draft, save the result into the library.
function WorkoutChatDialog({ open, onClose, onSaved }: WorkoutChatDialogProps) {
  const { t } = useTranslation('stride')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [saving, setSaving] = useState(false)
  const [draft, setDraft] = useState<StrideWorkout | null>(null)
  const [chatError, setChatError] = useState('')
  const scrollRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight })
  }, [messages, draft])

  const send = useCallback(async () => {
    const text = input.trim()
    if (!text || busy) return
    const next: ChatMessage[] = [...messages, { role: 'user', content: text }]
    setMessages(next)
    setInput('')
    setBusy(true)
    setChatError('')
    try {
      const res = await fetch('/api/stride/workouts/generate', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ messages: next }),
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setChatError(data.error ?? t('library.chat.error'))
        return
      }
      if (data.reply) {
        setMessages((prev) => [...prev, { role: 'coach', content: data.reply }])
      }
      if (data.workout) setDraft(data.workout)
    } catch {
      setChatError(t('library.chat.error'))
    } finally {
      setBusy(false)
    }
  }, [busy, input, messages, t])

  const saveDraft = useCallback(async () => {
    if (!draft || saving) return
    setSaving(true)
    setChatError('')
    try {
      const res = await fetch('/api/stride/workouts', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(draft),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setChatError(data.error ?? t('library.chat.saveError'))
        return
      }
      setMessages([])
      setDraft(null)
      onSaved()
    } catch {
      setChatError(t('library.chat.saveError'))
    } finally {
      setSaving(false)
    }
  }, [draft, onSaved, saving, t])

  return (
    <Dialog open={open} onClose={onClose} aria-label={t('library.chat.title')}>
      <DialogHeader title={t('library.chat.title')} onClose={onClose} />
      <DialogBody>
        <div ref={scrollRef} className="max-h-80 overflow-y-auto space-y-3 pr-1">
          {messages.length === 0 && (
            <p className="text-sm text-gray-400">{t('library.chat.intro')}</p>
          )}
          {messages.map((m, i) => (
            <div
              key={i}
              className={`rounded-lg px-3 py-2 text-sm whitespace-pre-wrap ${
                m.role === 'user'
                  ? 'bg-purple-600/20 text-purple-100 ml-8'
                  : 'bg-gray-800 text-gray-200 mr-8'
              }`}
            >
              {m.content}
            </div>
          ))}
          {draft && (
            <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 px-3 py-2 text-sm">
              <p className="font-medium text-yellow-200">{draft.name}</p>
              <p className="text-gray-300 mt-1">{draft.main_set}</p>
              {(draft.warmup || draft.cooldown) && (
                <p className="text-gray-500 text-xs mt-1">
                  {draft.warmup}{draft.warmup && draft.cooldown ? ' · ' : ''}{draft.cooldown}
                </p>
              )}
              {draft.blocks?.length > 0 && (
                <p className="text-gray-500 text-xs mt-1">
                  {draft.blocks.map((b) => t(`library.blocks.${b}`, { defaultValue: b })).join(', ')}
                </p>
              )}
              <button
                type="button"
                onClick={() => void saveDraft()}
                disabled={saving}
                className="mt-2 flex items-center gap-1.5 px-3 py-1.5 text-sm bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white rounded-lg transition-colors"
              >
                {saving ? <Loader2 size={14} className="animate-spin" /> : <Dumbbell size={14} />}
                {t('library.chat.save')}
              </button>
            </div>
          )}
          {busy && (
            <div className="flex items-center gap-2 text-sm text-gray-400">
              <Loader2 size={14} className="animate-spin" />
              {t('library.chat.thinking')}
            </div>
          )}
        </div>
        {chatError && <p className="text-sm text-red-400 mt-2">{chatError}</p>}
        <form
          className="mt-3 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            void send()
          }}
        >
          <input
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={t('library.chat.placeholder')}
            className="flex-1 px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-purple-400"
          />
          <button
            type="submit"
            disabled={busy || input.trim() === ''}
            aria-label={t('library.chat.send')}
            className="px-3 py-2 bg-purple-600 hover:bg-purple-700 disabled:opacity-50 text-white rounded-lg transition-colors"
          >
            <Send size={16} />
          </button>
        </form>
      </DialogBody>
    </Dialog>
  )
}
