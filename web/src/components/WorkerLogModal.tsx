import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'

interface WorkerLogModalProps {
  open: boolean
  onClose: () => void
  workerId: string | null
  beadId: string
}

export default function WorkerLogModal({ open, onClose, workerId, beadId }: WorkerLogModalProps) {
  const { t } = useTranslation('forge')
  const [lines, setLines] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const prevFocusRef = useRef<Element | null>(null)
  const scrollRef = useRef<HTMLPreElement>(null)

  useEffect(() => {
    if (open) {
      prevFocusRef.current = document.activeElement
      closeRef.current?.focus()
    } else {
      if (prevFocusRef.current instanceof HTMLElement) {
        prevFocusRef.current.focus()
        prevFocusRef.current = null
      }
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, onClose])

  useEffect(() => {
    if (!open || !workerId) return
    let cancelled = false
    setLoading(true)
    setError(null)
    setLines([])

    fetch(`/api/forge/workers/${encodeURIComponent(workerId)}/log?tail=200`, {
      credentials: 'include',
    })
      .then(async res => {
        if (cancelled) return
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          setError((data as { error?: string }).error ?? `HTTP ${res.status}`)
          return
        }
        const data: { lines: string[] } = await res.json()
        if (!cancelled) {
          setLines(data.lines ?? [])
          requestAnimationFrame(() => {
            if (scrollRef.current) {
              scrollRef.current.scrollTop = scrollRef.current.scrollHeight
            }
          })
        }
      })
      .catch(err => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : t('unknownError'))
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => { cancelled = true }
  }, [open, workerId, t])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="worker-log-title"
    >
      <div
        className="absolute inset-0 bg-black/60"
        onClick={onClose}
        aria-hidden="true"
      />

      <div className="relative z-10 w-full max-w-3xl max-h-[80vh] rounded-xl bg-gray-800 border border-gray-700 shadow-2xl flex flex-col">
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700">
          <h2 id="worker-log-title" className="text-base font-semibold text-white">
            {t('attention.viewLogsTitle', { id: beadId })}
          </h2>
          <button
            ref={closeRef}
            type="button"
            onClick={onClose}
            aria-label={t('beadDetail.back')}
            className="min-h-[36px] min-w-[36px] flex items-center justify-center rounded-lg text-gray-400 hover:text-white hover:bg-gray-700 transition-colors"
          >
            <X size={18} />
          </button>
        </div>

        <pre
          ref={scrollRef}
          className="flex-1 overflow-auto px-5 py-4 text-xs text-gray-300 font-mono leading-relaxed whitespace-pre-wrap break-words"
        >
          {loading && <span className="text-gray-500">{t('beadDetail.loading')}</span>}
          {error && <span className="text-red-400">{error}</span>}
          {!loading && !error && lines.length === 0 && (
            <span className="text-gray-500">{t('liveActivity.noOutput')}</span>
          )}
          {lines.map((line, i) => (
            <div key={i}>{line}</div>
          ))}
        </pre>
      </div>
    </div>
  )
}
