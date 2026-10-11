import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Sparkles, Trash2, X } from 'lucide-react'
import { ChatComposer, ChatMessageList } from '../../components/chat/ChatThread'
import { type ChatMessage, type Trip, askTripChat, clearTripChat, fetchTripChat, toLang } from './tripsApi'

const POLL_MS = 2500

/** Ask Claude about the trip, or have it change things. One shared conversation per trip. */
export function TripChat({ trip, onTripChanged }: { trip: Trip; onTripChanged: () => void }) {
  const { t, i18n } = useTranslation('trips')
  const lang = toLang(i18n.language)
  const [open, setOpen] = useState(false)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [sending, setSending] = useState(false)
  const seenChanges = useRef<Set<number>>(new Set())
  const bottom = useRef<HTMLDivElement>(null)
  const pending = messages.some(m => m.status === 'pending')

  // Load when opened; poll while Claude is answering.
  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    const load = () => fetchTripChat(trip.id, controller.signal).then(d => {
      setMessages(d.messages ?? [])
      for (const m of d.messages ?? []) {
        if (m.changed && !seenChanges.current.has(m.id)) {
          seenChanges.current.add(m.id)
          onTripChanged()
        }
      }
    }).catch(() => {})
    load()
    const timer = pending ? setInterval(load, POLL_MS) : undefined
    return () => { controller.abort(); if (timer) clearInterval(timer) }
  }, [open, pending, trip.id, onTripChanged])

  useEffect(() => { bottom.current?.scrollIntoView?.({ block: 'end' }) }, [messages, open])

  // Changes made before the panel was opened don't trigger a reload.
  useEffect(() => {
    if (open) return
    seenChanges.current = new Set()
  }, [open])

  const send = async (message: string) => {
    if (!message.trim() || pending || sending) return
    setSending(true)
    setError('')
    try {
      const d = await askTripChat(trip.id, message.trim(), lang)
      for (const m of d.messages ?? []) if (m.changed) seenChanges.current.add(m.id)
      setMessages(d.messages ?? [])
      setText('')
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.save'))
    } finally {
      setSending(false)
    }
  }

  const suggestions = [t('chat.suggest.left'), t('chat.suggest.leave'), t('chat.suggest.weather')]

  return (
    <>
      <button type="button" onClick={() => setOpen(true)} aria-expanded={open}
        className="fixed bottom-5 right-5 z-30 inline-flex items-center gap-2 rounded-full bg-blue-600 px-4 py-3 text-sm font-semibold shadow-lg hover:bg-blue-500 cursor-pointer">
        <Sparkles size={18} aria-hidden="true" /> {t('chat.open')}
      </button>
      {open && (
        <section role="dialog" aria-modal="false" aria-labelledby="trip-chat-h"
          className="fixed inset-0 z-40 flex flex-col bg-gray-950 sm:inset-auto sm:bottom-5 sm:right-5 sm:h-[36rem] sm:max-h-[85vh] sm:w-[26rem] sm:rounded-xl sm:border sm:border-gray-700 sm:shadow-2xl">
          <header className="flex items-center justify-between gap-2 border-b border-gray-800 px-4 py-3">
            <h2 id="trip-chat-h" className="inline-flex items-center gap-2 font-semibold"><Sparkles size={16} aria-hidden="true" /> {t('chat.title')}</h2>
            <div className="flex items-center gap-1">
              {trip.can_edit && messages.length > 0 && !pending && (
                <button type="button" aria-label={t('chat.clear')} title={t('chat.clear')} className="rounded p-1.5 text-gray-500 hover:text-red-300 cursor-pointer"
                  onClick={async () => { if (window.confirm(t('chat.confirmClear'))) { await clearTripChat(trip.id); setMessages([]) } }}>
                  <Trash2 size={15} />
                </button>
              )}
              <button type="button" aria-label={t('chat.close')} onClick={() => setOpen(false)} className="rounded p-1.5 text-gray-400 hover:text-white cursor-pointer"><X size={18} /></button>
            </div>
          </header>

          <div className="flex-1 space-y-3 overflow-y-auto px-4 py-3" aria-live="polite">
            {messages.length === 0 && (
              <div className="text-sm text-gray-400">
                <p>{trip.can_edit ? t('chat.introEdit') : t('chat.introView')}</p>
                <div className="mt-3 flex flex-wrap gap-2">
                  {suggestions.map(s => (
                    <button key={s} type="button" onClick={() => send(s)} className="rounded-full border border-gray-700 bg-gray-800 px-3 py-1.5 text-left text-xs text-gray-200 hover:bg-gray-700 cursor-pointer">{s}</button>
                  ))}
                </div>
              </div>
            )}
            <ChatMessageList thinkingLabel={t('chat.thinking')} messages={messages.map(m => ({
              id: m.id, mine: m.role === 'user', author: m.role === 'user' ? (m.user_name ?? '') : t('chat.title'),
              content: m.content, pending: m.status === 'pending', failed: m.status === 'failed',
              badge: m.changed ? t('chat.changed') : undefined,
            }))} />
            <div ref={bottom} />
          </div>

          {error && <p role="alert" className="px-4 pb-1 text-sm text-red-300">{error}</p>}
          <div className="border-t border-gray-800 p-3">
            <ChatComposer multiline value={text} onChange={setText} onSend={() => send(text)} busy={sending} disabled={pending}
              placeholder={t('chat.placeholder')} sendLabel={t('chat.send')} />
          </div>
        </section>
      )}
    </>
  )
}
