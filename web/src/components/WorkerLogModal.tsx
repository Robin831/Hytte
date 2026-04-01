import { useReducer, useEffect, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'

interface WorkerLogModalProps {
  open: boolean
  onClose: () => void
  workerId: string | null
  beadId: string
}

type LogState = { loading: boolean; error: string | null; lines: string[] }
type LogAction =
  | { type: 'reset' }
  | { type: 'success'; lines: string[] }
  | { type: 'error'; error: string }
  | { type: 'done' }

function logReducer(state: LogState, action: LogAction): LogState {
  switch (action.type) {
    case 'reset': return { loading: true, error: null, lines: [] }
    case 'success': return { ...state, lines: action.lines }
    case 'error': return { ...state, error: action.error }
    case 'done': return { ...state, loading: false }
    default: return state
  }
}

export default function WorkerLogModal({ open, onClose, workerId, beadId }: WorkerLogModalProps) {
  const { t } = useTranslation('forge')
  const [{ lines, loading, error }, dispatch] = useReducer(logReducer, { loading: false, error: null, lines: [] })
  const closeRef = useRef<HTMLButtonElement>(null)
  const prevFocusRef = useRef<Element | null>(null)
  const scrollRef = useRef<HTMLPreElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  const fetchKey = open ? workerId : null

  // Derived during render — avoids synchronous setState in an effect
  const noWorkerError = open && workerId === null ? t('attention.noWorkerFound') : null

  useEffect(() => {
    if (fetchKey === null) return
    // open && worker present — start fresh and show loading
    dispatch({ type: 'reset' })
  }, [fetchKey])

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

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      onClose()
      return
    }
    if (e.key === 'Tab' && dialogRef.current) {
      const focusable = dialogRef.current.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      )
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
  }, [onClose])

  useEffect(() => {
    if (!open) return
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, handleKeyDown])

  useEffect(() => {
    if (fetchKey === null) return
    const controller = new AbortController()

    fetch(`/api/forge/workers/${encodeURIComponent(fetchKey)}/log?tail=200`, {
      credentials: 'include',
      signal: controller.signal,
    })
      .then(async res => {
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          dispatch({ type: 'error', error: (data as { error?: string }).error ?? `HTTP ${res.status}` })
          return
        }
        const data: { lines: string[] } = await res.json()
        dispatch({ type: 'success', lines: data.lines ?? [] })
        requestAnimationFrame(() => {
          if (scrollRef.current) {
            scrollRef.current.scrollTop = scrollRef.current.scrollHeight
          }
        })
      })
      .catch(err => {
        if (err instanceof Error && err.name === 'AbortError') return
        dispatch({ type: 'error', error: err instanceof Error ? err.message : t('unknownError') })
      })
      .finally(() => {
        dispatch({ type: 'done' })
      })

    return () => { controller.abort() }
  }, [fetchKey, t])

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

      <div
        ref={dialogRef}
        className="relative z-10 w-full max-w-3xl max-h-[80vh] rounded-xl bg-gray-800 border border-gray-700 shadow-2xl flex flex-col"
      >
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
          {(noWorkerError || error) && <span className="text-red-400">{noWorkerError ?? error}</span>}
          {!loading && !noWorkerError && !error && lines.length === 0 && (
            <span className="text-gray-500">{t('liveActivity.noOutput')}</span>
          )}
          {lines.map((line, i) => (
            <span key={`${i}-${line.length}`}>{line}{'\n'}</span>
          ))}
        </pre>
      </div>
    </div>
  )
}
