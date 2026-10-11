import { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { MessageCircle, HelpCircle } from 'lucide-react'
import { ChatComposer, ChatMessageList } from '../chat/ChatThread'

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
          {messages && (
            <ChatMessageList compact thinkingLabel={t('evalThread.coachThinking')} messages={messages.map(m => ({
              id: m.id, mine: m.role !== 'coach', author: m.role === 'coach' ? t('evalThread.coach') : t('evalThread.you'),
              content: m.content, badge: m.eval_revised ? t('evalThread.evalRevised') : undefined,
            }))} />
          )}
          {messages !== null && messages.length === 0 && (
            <p className="text-xs text-gray-500">{t('evalThread.empty')}</p>
          )}

          <ChatComposer value={draft} onChange={setDraft} onSend={() => void send()} busy={sending} disabled={sending}
            placeholder={t('evalThread.placeholder')} sendLabel={t('evalThread.send')} />
          {sending && (
            <p className="text-xs text-gray-500">{t('evalThread.coachThinking')}</p>
          )}
        </div>
      )}
    </div>
  )
}
