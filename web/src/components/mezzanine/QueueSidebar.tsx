import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { ListOrdered, ChevronDown, ChevronRight } from 'lucide-react'
import { SECTION_ORDER } from '../forgeQueueUi'
import QueueItem from './QueueItem'

type ErrorKey = 'mezzanine.queueSidebar.unavailable' | 'mezzanine.queueSidebar.unknownError'

interface QueueBead {
  bead_id: string
  anvil: string
  title: string
  priority: number
  status: string
  section: string
}

function sectionIndex(s: string): number {
  const i = SECTION_ORDER.indexOf(s)
  return i === -1 ? SECTION_ORDER.length : i
}

interface AnvilGroup {
  anvil: string
  beads: QueueBead[]
}

function groupByAnvil(beads: QueueBead[]): AnvilGroup[] {
  const map = new Map<string, QueueBead[]>()
  for (const b of beads) {
    const list = map.get(b.anvil)
    if (list) {
      list.push(b)
    } else {
      map.set(b.anvil, [b])
    }
  }
  return Array.from(map.entries())
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([anvil, anvilBeads]) => ({
      anvil,
      beads: anvilBeads.sort((a, b) => {
        const sd = sectionIndex(a.section) - sectionIndex(b.section)
        if (sd !== 0) return sd
        const pd = a.priority - b.priority
        if (pd !== 0) return pd
        return a.bead_id.localeCompare(b.bead_id)
      }),
    }))
}

interface AnvilSectionProps {
  group: AnvilGroup
  onBeadClick?: (beadId: string) => void
}

function AnvilSection({ group, onBeadClick }: AnvilSectionProps) {
  const { t } = useTranslation('forge')
  const [open, setOpen] = useState(true)
  const sectionId = `anvil-section-${group.anvil.replace(/[^a-z0-9]/gi, '-').toLowerCase()}`

  return (
    <div className="border-b border-gray-700/40 last:border-0" role="region" aria-label={group.anvil}>
      <button
        type="button"
        onClick={() => setOpen(prev => !prev)}
        aria-expanded={open}
        aria-controls={sectionId}
        aria-label={t('mezzanine.queueSidebar.toggleAnvil', { anvil: group.anvil })}
        className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-gray-700/30 transition-colors"
      >
        {open ? (
          <ChevronDown size={14} className="text-gray-500 shrink-0" aria-hidden="true" />
        ) : (
          <ChevronRight size={14} className="text-gray-500 shrink-0" aria-hidden="true" />
        )}
        <span className="text-xs font-medium text-gray-300 truncate">{group.anvil}</span>
        <span className="ml-auto flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full bg-cyan-500/20 text-cyan-400 text-[10px] font-medium shrink-0">
          {group.beads.length}
        </span>
      </button>

      {open && (
        <ul id={sectionId} aria-label={t('mezzanine.queueSidebar.beadsInAnvil', { anvil: group.anvil })}>
          {group.beads.map(bead => (
            <QueueItem
              key={bead.bead_id}
              beadId={bead.bead_id}
              title={bead.title}
              priority={bead.priority}
              status={bead.status}
              section={bead.section}
              onBeadClick={onBeadClick}
            />
          ))}
        </ul>
      )}
    </div>
  )
}

interface QueueSidebarProps {
  onBeadClick?: (beadId: string) => void
}

export default function QueueSidebar({ onBeadClick }: QueueSidebarProps) {
  const { t } = useTranslation('forge')
  const [beads, setBeads] = useState<QueueBead[]>([])
  const [loading, setLoading] = useState(true)
  const [errorKey, setErrorKey] = useState<ErrorKey | null>(null)
  const [errorDetail, setErrorDetail] = useState<string | null>(null)

  const fetchQueue = useCallback(async (signal: AbortSignal): Promise<boolean> => {
    const res = await fetch('/api/forge/queue/all', {
      credentials: 'include',
      signal,
    })
    if (signal.aborted) return false
    if (res.status === 404) {
      setBeads([])
      setErrorKey('mezzanine.queueSidebar.unavailable')
      setErrorDetail(null)
      return false
    }
    if (!res.ok) {
      const data = await res.json().catch(() => ({}))
      const msg = (data as { error?: string }).error
      if (msg) {
        setErrorKey(null)
        setErrorDetail(msg)
      } else {
        setErrorKey(null)
        setErrorDetail(`HTTP ${res.status}`)
      }
    } else {
      const data: QueueBead[] = await res.json()
      setBeads(data)
      setErrorKey(null)
      setErrorDetail(null)
    }
    return true
  }, [])

  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    const controller = new AbortController()

    async function poll() {
      let shouldContinue = true
      try {
        shouldContinue = await fetchQueue(controller.signal)
      } catch (err) {
        if (controller.signal.aborted || (err instanceof Error && err.name === 'AbortError')) return
        if (err instanceof Error) {
          setErrorKey(null)
          setErrorDetail(err.message)
        } else {
          setErrorKey('mezzanine.queueSidebar.unknownError')
          setErrorDetail(null)
        }
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          if (shouldContinue) {
            timeoutId = setTimeout(() => void poll(), 5000)
          }
        }
      }
    }

    void poll()
    return () => {
      controller.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [fetchQueue])

  const groups = groupByAnvil(beads)
  const totalBeads = beads.length
  const errorMessage = errorKey ? String(t(errorKey)) : errorDetail

  // Announce brief status updates to screen readers after each poll
  const announcement = loading ? '' : (errorMessage || String(t('mezzanine.queueSidebar.queueUpdated', { count: totalBeads })))

  return (
    <nav className="flex flex-col h-full" aria-label={t('mezzanine.queueSidebar.title')}>
      {/* Visually-hidden live region for brief status announcements */}
      <div
        role="status"
        aria-live="polite"
        aria-atomic="true"
        className="sr-only"
      >
        {announcement}
      </div>

      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2.5 border-b border-gray-700/50">
        <ListOrdered size={16} className="text-cyan-400 shrink-0" aria-hidden="true" />
        <span className="text-sm font-medium text-gray-200">
          {t('mezzanine.queueSidebar.title')}
        </span>
        {totalBeads > 0 && (
          <span className="ml-auto text-xs text-gray-500">
            {t('mezzanine.queueSidebar.totalBeads', { count: totalBeads })}
          </span>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto">
        {loading ? (
          <p className="px-3 py-6 text-xs text-gray-500 text-center">
            {t('mezzanine.queueSidebar.loading')}
          </p>
        ) : errorMessage ? (
          <p className="px-3 py-6 text-xs text-red-400 text-center" role="alert">{errorMessage}</p>
        ) : groups.length === 0 ? (
          <p className="px-3 py-6 text-xs text-gray-500 text-center">
            {t('mezzanine.queueSidebar.empty')}
          </p>
        ) : (
          groups.map(g => (
            <AnvilSection key={g.anvil} group={g} onBeadClick={onBeadClick} />
          ))
        )}
      </div>
    </nav>
  )
}
