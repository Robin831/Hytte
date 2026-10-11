import { Loader2, Send, Sparkles } from 'lucide-react'

// Shared building blocks for Hytte's assistant threads — Stride's coach
// thread and the trip chat: a message list (mine vs the assistant's, a
// pending "thinking" bubble, a badge when the reply changed something) and
// a composer. Each feature keeps its own data flow and labels.

export interface ThreadMessage {
  id: number
  mine: boolean
  author: string
  content: string
  pending?: boolean
  failed?: boolean
  /** Shown under the message, e.g. "Trip updated". */
  badge?: string
  /** Pictures attached to the message (URLs). */
  images?: string[]
}

export function ChatMessageList({ messages, thinkingLabel, compact = false, pictureLabel = '' }: {
  messages: ThreadMessage[]
  thinkingLabel: string
  compact?: boolean
  /** Alt text for attached pictures. */
  pictureLabel?: string
}) {
  return (
    <>
      {messages.map(m => (
        <div key={m.id} className={compact ? '' : `flex ${m.mine ? 'justify-end' : 'justify-start'}`}>
          <div className={`rounded-lg px-3 py-2 text-sm ${compact ? '' : 'max-w-[85%] rounded-2xl'} ${
            m.mine
              ? compact ? 'ml-4 border border-blue-800/50 bg-blue-900/30 text-blue-100' : 'bg-blue-600 text-white'
              : m.failed ? 'bg-red-950/60 text-red-200' : 'bg-gray-800/80 text-gray-100'
          }`}>
            {m.author && <span className={`mb-0.5 block text-[10px] uppercase tracking-wide ${m.mine && !compact ? 'text-blue-100/80' : 'text-gray-500'}`}>{m.author}</span>}
            {m.images && m.images.length > 0 && (
              <span className="mb-1 flex flex-wrap gap-1.5">
                {m.images.map(src => (
                  <a key={src} href={src} target="_blank" rel="noopener noreferrer">
                    <img src={src} alt={pictureLabel} loading="lazy" className="h-24 w-24 rounded-lg object-cover" />
                  </a>
                ))}
              </span>
            )}
            {m.pending ? (
              <span className="inline-flex items-center gap-2 text-gray-400"><Loader2 size={14} className="animate-spin" aria-hidden="true" /> {thinkingLabel}</span>
            ) : (
              <span className="whitespace-pre-wrap break-words">{m.content}</span>
            )}
            {m.badge && (
              <span className="mt-1 flex items-center gap-1 text-[11px] text-emerald-400"><Sparkles size={11} aria-hidden="true" />{m.badge}</span>
            )}
          </div>
        </div>
      ))}
    </>
  )
}

export function ChatComposer({ value, onChange, onSend, busy, disabled, placeholder, sendLabel, multiline = false, maxLength = 2000, leading, allowEmpty = false }: {
  value: string
  onChange: (v: string) => void
  onSend: () => void
  busy: boolean
  disabled?: boolean
  placeholder: string
  sendLabel: string
  multiline?: boolean
  maxLength?: number
  /** A control before the text field, e.g. an attach button. */
  leading?: React.ReactNode
  /** Allow sending without text (when something is attached). */
  allowEmpty?: boolean
}) {
  const field = 'flex-1 min-w-0 rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white placeholder-gray-500 focus:border-blue-500 focus:outline-none disabled:opacity-60'
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); onSend() }
  }
  return (
    <form className="flex gap-2" onSubmit={e => { e.preventDefault(); onSend() }}>
      {leading}
      {multiline ? (
        <textarea value={value} rows={2} maxLength={maxLength} onChange={e => onChange(e.target.value)} onKeyDown={onKeyDown}
          placeholder={placeholder} aria-label={placeholder} disabled={disabled} className={`${field} resize-none`} />
      ) : (
        <input type="text" value={value} onChange={e => onChange(e.target.value)} onKeyDown={onKeyDown}
          placeholder={placeholder} aria-label={placeholder} disabled={disabled} className={field} />
      )}
      <button type="submit" disabled={(!value.trim() && !allowEmpty) || busy || disabled} aria-label={sendLabel}
        className="shrink-0 self-end rounded-lg bg-blue-600 px-3 py-2 text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50 cursor-pointer">
        {busy ? <Loader2 size={16} className="animate-spin" /> : <Send size={16} />}
      </button>
    </form>
  )
}
