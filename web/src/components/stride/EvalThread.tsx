import { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { MessageCircle, Send, Loader2, Sparkles, HelpCircle } from 'lucide-react'

interface EvalMessage {
  id: number
  role: 'user' | 'coach'
  content: string
  eval_revised: boolean
  created_at: string
}

interface EvalThreadProps {
  evaluationId: number
  /** Clarifying questions from the stored evaluation, shown as a hint chip. */
  questions?: string[]
  /** Called when the coach revises the evaluation so the parent can refetch. */
  onEvalRevised?: () => void
}

// EvalThread is the per-evaluation coach conversation: answer the coach's
// clarifying questions or correct the assessment, and get a short reply —
// possibly with an in-place revision of the evaluation — without triggering
// a full re-evaluation.
export default function EvalThread({ evaluationId, questions, onEvalRevised }: EvalThreadProps) {
  const { t } = useTranslation('stride')
  const [open, setOpen] = useState(false)
  const [messages, setMessages] = useState<EvalMessage[] | null>(null)
  const [draft, setDraft] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const res = await fetch(`/api/stride/evaluations/${evaluationId}/messages`, { credentials: 'include' })
      if (!res.ok) throw new Error('load failed')
      const data = await res.json()
      setMessages(data.messages as EvalMessage[])
    } catch {
      setError(t('evalThread.errors.failedToLoad'))
    }
  }, [evaluationId, t])

  const toggle = () => {
    const next = !open
    setOpen(next)
    if (next && messages === null) void load()
  }

  const send = async () => {
    const content = draft.trim()
    if (!content || sending) return
    setSending(true)
    setError('')
    try {
      const res = await fetch(`/api/stride/evaluations/${evaluationId}/messages`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content }),
      })
      if (!res.ok) throw new Error('send failed')
      const data = (await res.json()) as { messages: EvalMessage[]; updated_eval?: unknown }
      setMessages(prev => [...(prev ?? []), ...data.messages])
      setDraft('')
      if (data.updated_eval && onEvalRevised) onEvalRevised()
    } catch {
      setError(t('evalThread.errors.failedToSend'))
    } finally {
      setSending(false)
    }
  }

  const hasQuestions = (questions?.length ?? 0) > 0

  return (
    <div className="mt-3 border-t border-gray-700/60 pt-2">
      <button
        onClick={toggle}
        aria-expanded={open}
        className={`flex items-center gap-1.5 text-xs cursor-pointer transition-colors ${
          hasQuestions ? 'text-amber-300 hover:text-amber-200' : 'text-gray-400 hover:text-gray-200'
        }`}
      >
        {hasQuestions ? <HelpCircle size={14} /> : <MessageCircle size={14} />}
        {hasQuestions ? t('evalThread.coachAsked') : t('evalThread.discuss')}
      </button>

      {open && (
        <div className="mt-2 space-y-2">
          {error && (
            <p role="alert" className="text-xs text-red-400">{error}</p>
          )}
          {messages === null && !error && (
            <p className="text-xs text-gray-500">{t('evalThread.loading')}</p>
          )}
          {messages?.map(m => (
            <div
              key={m.id}
              className={`rounded-lg px-3 py-2 text-sm ${
                m.role === 'coach'
                  ? 'bg-gray-800/80 text-gray-200'
                  : 'bg-blue-900/30 border border-blue-800/50 text-blue-100 ml-4'
              }`}
            >
              <span className="block text-[10px] uppercase tracking-wide text-gray-500 mb-0.5">
                {m.role === 'coach' ? t('evalThread.coach') : t('evalThread.you')}
              </span>
              <span className="whitespace-pre-wrap break-words">{m.content}</span>
              {m.eval_revised && (
                <span className="mt-1 flex items-center gap-1 text-[11px] text-emerald-400">
                  <Sparkles size={11} />
                  {t('evalThread.evalRevised')}
                </span>
              )}
            </div>
          ))}
          {messages !== null && messages.length === 0 && (
            <p className="text-xs text-gray-500">{t('evalThread.empty')}</p>
          )}

          <div className="flex gap-2">
            <input
              type="text"
              value={draft}
              onChange={e => setDraft(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') void send() }}
              placeholder={t('evalThread.placeholder')}
              aria-label={t('evalThread.placeholder')}
              disabled={sending}
              className="flex-1 min-w-0 bg-gray-900 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500 disabled:opacity-60"
            />
            <button
              onClick={() => void send()}
              disabled={!draft.trim() || sending}
              aria-label={t('evalThread.send')}
              className="shrink-0 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg px-3 py-1.5 cursor-pointer transition-colors"
            >
              {sending ? <Loader2 size={16} className="animate-spin" /> : <Send size={16} />}
            </button>
          </div>
          {sending && (
            <p className="text-xs text-gray-500">{t('evalThread.coachThinking')}</p>
          )}
        </div>
      )}
    </div>
  )
}
