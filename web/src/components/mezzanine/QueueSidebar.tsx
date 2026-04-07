import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { ListOrdered, ChevronDown, ChevronRight } from 'lucide-react'
import QueueItem from './QueueItem'

interface QueueBead {
  bead_id: string
  anvil: string
  title: string
  priority: number
  status: string
  section: string
}

const SECTION_ORDER = ['ready', 'in-progress', 'unlabeled', 'needs-attention']

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
        return a.priority - b.priority
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

  return (
    <div className="border-b border-gray-700/40 last:border-0">
      <button
        type="button"
        onClick={() => setOpen(prev => !prev)}
        aria-expanded={open}
        aria-label={t('queue.toggleAnvil', { anvil: group.anvil })}
        className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-gray-700/30 transition-colors"
      >
        {open ? (
          <ChevronDown size={14} className="text-gray-500 shrink-0" />
        ) : (
          <ChevronRight size={14} className="text-gray-500 shrink-0" />
        )}
        <span className="text-xs font-medium text-gray-300 truncate">{group.anvil}</span>
        <span className="ml-auto flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full bg-cyan-500/20 text-cyan-400 text-[10px] font-medium shrink-0">
          {group.beads.length}
        </span>
      </button>

      {open && (
        <ul>
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
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    let controller: AbortController | null = null

    async function poll() {
      controller = new AbortController()
      let stopPolling = false
      try {
        const res = await fetch('/api/forge/queue/all', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (cancelled) return
        if (res.status === 404) {
          setBeads([])
          setError(t('mezzanine.queueSidebar.unavailable'))
          stopPolling = true
          return
        }
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          setError((data as { error?: string }).error ?? `HTTP ${res.status}`)
        } else {
          const data: QueueBead[] = await res.json()
          setBeads(data)
          setError(null)
        }
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
        setError(err instanceof Error ? err.message : t('unknownError'))
      } finally {
        if (!cancelled) {
          setLoading(false)
          if (!stopPolling) {
            timeoutId = setTimeout(() => void poll(), 5000)
          }
        }
      }
    }

    void poll()
    return () => {
      cancelled = true
      controller?.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [t])

  const groups = groupByAnvil(beads)
  const totalBeads = beads.length

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2.5 border-b border-gray-700/50">
        <ListOrdered size={16} className="text-cyan-400 shrink-0" />
        <span className="text-sm font-medium text-gray-200">
          {t('mezzanine.queueSidebar.title')}
        </span>
        {totalBeads > 0 && (
          <span className="ml-auto text-xs text-gray-500">
            {t('queue.totalBeads', { total: totalBeads })}
          </span>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto">
        {loading ? (
          <p className="px-3 py-6 text-xs text-gray-500 text-center">
            {t('mezzanine.queueSidebar.loading')}
          </p>
        ) : error ? (
          <p className="px-3 py-6 text-xs text-red-400 text-center">{error}</p>
        ) : groups.length === 0 ? (
          <p className="px-3 py-6 text-xs text-gray-500 text-center">
            {t('queue.empty')}
          </p>
        ) : (
          groups.map(g => (
            <AnvilSection key={g.anvil} group={g} onBeadClick={onBeadClick} />
          ))
        )}
      </div>
    </div>
  )
}
