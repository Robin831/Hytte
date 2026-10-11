import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Brain, ImagePlus, Sparkles, Trash2, X } from 'lucide-react'
import { ChatComposer, ChatMessageList } from '../../components/chat/ChatThread'
import {
  type AssistantNote, type ChatMessage, type Trip, askTripChat, clearTripChat, deleteAssistantNote, fetchAssistantNotes,
  fetchTripChat, toLang,
} from './tripsApi'
import { MAX_CHAT_PICTURES, pictureToDataURL } from './pictures'

const POLL_MS = 2500

/** Ask Claude about the trip, or have it change things. One shared conversation per trip. */
export function TripChat({ trip, onTripChanged }: { trip: Trip; onTripChanged: () => void }) {
  const { t, i18n } = useTranslation('trips')
  const lang = toLang(i18n.language)
  const [open, setOpen] = useState(false)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [text, setText] = useState('')
  const [pictures, setPictures] = useState<string[]>([])
  const [dragging, setDragging] = useState(false)
  const [error, setError] = useState('')
  const [sending, setSending] = useState(false)
  const [notes, setNotes] = useState<AssistantNote[] | null>(null)
  const [showNotes, setShowNotes] = useState(false)
  const seenChanges = useRef<Set<number>>(new Set())
  const bottom = useRef<HTMLDivElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)
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

  // What Claude remembers: loaded when shown, and again after each answer.
  useEffect(() => {
    if (!open || !showNotes || pending) return
    const controller = new AbortController()
    fetchAssistantNotes(controller.signal).then(d => setNotes(d.notes ?? [])).catch(() => {})
    return () => controller.abort()
  }, [open, showNotes, pending])

  useEffect(() => { bottom.current?.scrollIntoView?.({ block: 'end' }) }, [messages, open])

  // Changes made before the panel was opened don't trigger a reload.
  useEffect(() => {
    if (open) return
    seenChanges.current = new Set()
  }, [open])

  const addPictures = async (files: Iterable<File>) => {
    const images = [...files].filter(f => f.type.startsWith('image/'))
    if (images.length === 0) return
    setError('')
    try {
      const urls = await Promise.all(images.map(pictureToDataURL))
      setPictures(prev => {
        const next = [...prev, ...urls]
        if (next.length > MAX_CHAT_PICTURES) setError(t('chat.tooManyPictures', { max: MAX_CHAT_PICTURES }))
        return next.slice(0, MAX_CHAT_PICTURES)
      })
    } catch {
      setError(t('chat.pictureFailed'))
    }
  }

  const send = async (message: string) => {
    if ((!message.trim() && pictures.length === 0) || pending || sending) return
    setSending(true)
    setError('')
    try {
      const d = await askTripChat(trip.id, message.trim(), lang, pictures)
      for (const m of d.messages ?? []) if (m.changed) seenChanges.current.add(m.id)
      setMessages(d.messages ?? [])
      setText('')
      setPictures([])
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
          onDragOver={e => { if (e.dataTransfer?.types?.includes('Files')) { e.preventDefault(); setDragging(true) } }}
          onDragLeave={e => { if (e.currentTarget === e.target) setDragging(false) }}
          onDrop={e => { e.preventDefault(); setDragging(false); void addPictures(e.dataTransfer?.files ?? []) }}
          onPaste={e => { if (e.clipboardData?.files?.length) void addPictures(e.clipboardData.files) }}
          className={`fixed inset-0 z-40 flex flex-col bg-gray-950 sm:inset-auto sm:bottom-5 sm:right-5 sm:h-[36rem] sm:max-h-[85vh] sm:w-[26rem] sm:rounded-xl sm:border sm:shadow-2xl ${dragging ? 'sm:border-blue-500 ring-2 ring-blue-500' : 'sm:border-gray-700'}`}>
          <header className="flex items-center justify-between gap-2 border-b border-gray-800 px-4 py-3">
            <h2 id="trip-chat-h" className="inline-flex items-center gap-2 font-semibold"><Sparkles size={16} aria-hidden="true" /> {t('chat.title')}</h2>
            <div className="flex items-center gap-1">
              <button type="button" aria-label={t('chat.memory')} title={t('chat.memory')} aria-pressed={showNotes} onClick={() => setShowNotes(v => !v)}
                className={`rounded p-1.5 cursor-pointer ${showNotes ? 'text-blue-300' : 'text-gray-500 hover:text-gray-200'}`}><Brain size={16} /></button>
              {trip.can_edit && messages.length > 0 && !pending && (
                <button type="button" aria-label={t('chat.clear')} title={t('chat.clear')} className="rounded p-1.5 text-gray-500 hover:text-red-300 cursor-pointer"
                  onClick={async () => { if (window.confirm(t('chat.confirmClear'))) { await clearTripChat(trip.id); setMessages([]) } }}>
                  <Trash2 size={15} />
                </button>
              )}
              <button type="button" aria-label={t('chat.close')} onClick={() => setOpen(false)} className="rounded p-1.5 text-gray-400 hover:text-white cursor-pointer"><X size={18} /></button>
            </div>
          </header>

          {showNotes && (
            <div className="max-h-48 overflow-y-auto border-b border-gray-800 bg-gray-900/60 px-4 py-3 text-sm" role="region" aria-label={t('chat.memory')}>
              <p className="text-xs text-gray-400">{t('chat.memoryAbout')}</p>
              {notes && notes.length === 0 && <p className="mt-2 text-gray-500">{t('chat.memoryEmpty')}</p>}
              <ul className="mt-2 space-y-1">
                {(notes ?? []).map(n => (
                  <li key={n.id} className="flex items-start justify-between gap-2">
                    <span>{n.text}</span>
                    <button type="button" aria-label={t('chat.forget', { text: n.text })} className="shrink-0 text-gray-500 hover:text-red-300 cursor-pointer"
                      onClick={async () => { await deleteAssistantNote(n.id); setNotes(prev => (prev ?? []).filter(x => x.id !== n.id)) }}>
                      <X size={14} />
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="flex-1 space-y-3 overflow-y-auto px-4 py-3" aria-live="polite">
            {dragging && <p className="rounded-lg border border-dashed border-blue-500 p-4 text-center text-sm text-blue-200">{t('chat.dropHere')}</p>}
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
            <ChatMessageList thinkingLabel={t('chat.thinking')} pictureLabel={t('chat.picture')} messages={messages.map(m => ({
              id: m.id, mine: m.role === 'user', author: m.role === 'user' ? (m.user_name ?? '') : t('chat.title'),
              content: m.content, pending: m.status === 'pending', failed: m.status === 'failed',
              badge: m.changed ? t('chat.changed') : undefined,
              images: (m.images ?? []).map(id => `/api/trips/${trip.id}/chat/images/${id}`),
            }))} />
            <div ref={bottom} />
          </div>

          {error && <p role="alert" className="px-4 pb-1 text-sm text-red-300">{error}</p>}
          <div className="border-t border-gray-800 p-3">
            {pictures.length > 0 && (
              <ul className="mb-2 flex flex-wrap gap-2" aria-label={t('chat.attached')}>
                {pictures.map((src, i) => (
                  <li key={i} className="relative">
                    <img src={src} alt={t('chat.picture')} className="h-16 w-16 rounded-lg object-cover" />
                    <button type="button" aria-label={t('chat.removePicture')} onClick={() => setPictures(p => p.filter((_, j) => j !== i))}
                      className="absolute -right-1.5 -top-1.5 rounded-full bg-gray-800 p-0.5 text-gray-200 hover:bg-red-700 cursor-pointer"><X size={12} /></button>
                  </li>
                ))}
              </ul>
            )}
            <input ref={fileInput} type="file" accept="image/*" multiple hidden data-testid="chat-pictures"
              onChange={e => { void addPictures(e.target.files ?? []); e.target.value = '' }} />
            <ChatComposer multiline maxLength={20000} value={text} onChange={setText} onSend={() => send(text)} busy={sending} disabled={pending}
              placeholder={t('chat.placeholder')} sendLabel={t('chat.send')} allowEmpty={pictures.length > 0}
              leading={
                <button type="button" aria-label={t('chat.addPicture')} title={t('chat.addPicture')} disabled={pending}
                  onClick={() => fileInput.current?.click()}
                  className="shrink-0 self-end rounded-lg bg-gray-800 p-2.5 text-gray-300 hover:bg-gray-700 disabled:opacity-50 cursor-pointer"><ImagePlus size={16} /></button>
              } />
          </div>
        </section>
      )}
    </>
  )
}
