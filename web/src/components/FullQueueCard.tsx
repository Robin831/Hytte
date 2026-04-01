import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ListOrdered,
  ChevronDown,
  ChevronRight,
  Tag,
  Plus,
  X,
} from 'lucide-react'
import ConfirmDialog from './ConfirmDialog'

interface QueueBead {
  bead_id: string
  anvil: string
  title: string
  priority: number
  status: string
  labels: string
  section: string
  assignee: string
  description: string
  updated_at: string
}

interface AnvilGroup {
  anvil: string
  sections: SectionGroup[]
  totalBeads: number
}

interface SectionGroup {
  section: string
  beads: QueueBead[]
}

function parseLabels(raw: string): string[] {
  if (!raw) return []
  return raw
    .split(',')
    .map(l => l.trim())
    .filter(l => l.length > 0)
}

const FORGE_READY_LABEL = 'forgeReady'

const SECTION_ORDER = ['ready', 'in-progress', 'unlabeled', 'needs-attention']

function compareSections(a: string, b: string): number {
  const ai = SECTION_ORDER.indexOf(a)
  const bi = SECTION_ORDER.indexOf(b)
  if (ai === -1 && bi === -1) return a.localeCompare(b)
  if (ai === -1) return 1
  if (bi === -1) return -1
  return ai - bi
}

function groupByAnvilAndSection(beads: QueueBead[]): AnvilGroup[] {
  const anvilMap = new Map<string, Map<string, QueueBead[]>>()
  for (const bead of beads) {
    if (!anvilMap.has(bead.anvil)) {
      anvilMap.set(bead.anvil, new Map())
    }
    const sectionMap = anvilMap.get(bead.anvil)!
    if (!sectionMap.has(bead.section)) {
      sectionMap.set(bead.section, [])
    }
    sectionMap.get(bead.section)!.push(bead)
  }

  const result: AnvilGroup[] = []
  for (const [anvil, sectionMap] of Array.from(anvilMap.entries()).sort((a, b) =>
    a[0].localeCompare(b[0])
  )) {
    const sections: SectionGroup[] = Array.from(sectionMap.entries())
      .sort((a, b) => compareSections(a[0], b[0]))
      .map(([section, sectionBeads]) => ({ section, beads: sectionBeads }))
    const totalBeads = sections.reduce((s, sg) => s + sg.beads.length, 0)
    result.push({ anvil, sections, totalBeads })
  }
  return result
}

interface LabelActionState {
  beadId: string
  label: string
  action: 'add' | 'remove'
}

interface BeadRowProps {
  bead: QueueBead
  onLabelAction: (state: LabelActionState) => void
  pendingLabels: Record<string, boolean>
}

function BeadRow({ bead, onLabelAction, pendingLabels }: BeadRowProps) {
  const { t } = useTranslation('forge')
  const labels = parseLabels(bead.labels)
  const hasForgeReady = labels.includes(FORGE_READY_LABEL)
  const isPending = Object.keys(pendingLabels).some(k => k.startsWith(bead.bead_id + ':'))

  return (
    <li className="flex flex-col gap-1.5 py-2.5 border-b border-gray-700/30 last:border-0">
      {/* Top row: ID + title + priority + status */}
      <div className="flex items-start gap-2 min-w-0">
        <span className="text-xs font-mono text-cyan-400 shrink-0 pt-0.5">{bead.bead_id}</span>
        {bead.priority > 0 && (
          <span className="text-xs text-gray-600 shrink-0 pt-0.5">P{bead.priority}</span>
        )}
        {bead.title && (
          <span className="text-xs text-gray-300 truncate">{bead.title}</span>
        )}
        {bead.status && (
          <span className="ml-auto text-xs text-gray-500 shrink-0 pt-0.5 italic">{bead.status}</span>
        )}
      </div>

      {/* Labels row */}
      <div className="flex items-center flex-wrap gap-1.5">
        {labels.map(label => (
          <span
            key={label}
            className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs bg-gray-700/60 text-gray-400 border border-gray-600/40"
          >
            <Tag size={10} className="shrink-0" />
            {label}
            <button
              type="button"
              onClick={() => onLabelAction({ beadId: bead.bead_id, label, action: 'remove' })}
              disabled={isPending}
              aria-label={t('fullQueue.removeLabelLabel', { label, id: bead.bead_id })}
              className="hover:text-red-400 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <X size={10} />
            </button>
          </span>
        ))}

        {/* Add forgeReady button when not present */}
        {!hasForgeReady && (
          <button
            type="button"
            onClick={() =>
              onLabelAction({ beadId: bead.bead_id, label: FORGE_READY_LABEL, action: 'add' })
            }
            disabled={isPending}
            aria-label={t('fullQueue.addForgeReadyLabel', { id: bead.bead_id })}
            className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs bg-cyan-900/30 text-cyan-500 border border-cyan-700/30
              hover:bg-cyan-900/50 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Plus size={10} className="shrink-0" />
            {FORGE_READY_LABEL}
          </button>
        )}
      </div>
    </li>
  )
}

interface SectionBadgeProps {
  section: string
}

function SectionBadge({ section }: SectionBadgeProps) {
  const { t } = useTranslation('forge')
  const config: Record<string, { cls: string }> = {
    ready: { cls: 'bg-green-500/20 text-green-400 border-green-700/30' },
    'in-progress': { cls: 'bg-blue-500/20 text-blue-400 border-blue-700/30' },
    unlabeled: { cls: 'bg-gray-500/20 text-gray-400 border-gray-600/30' },
    'needs-attention': { cls: 'bg-amber-500/20 text-amber-400 border-amber-700/30' },
  }
  const { cls } = config[section] ?? { cls: 'bg-gray-500/20 text-gray-400 border-gray-600/30' }
  return (
    <span
      className={`inline-flex items-center px-1.5 py-0.5 rounded text-xs border font-medium ${cls}`}
    >
      {t(`fullQueue.section.${section}`, { defaultValue: section })}
    </span>
  )
}

interface AnvilSectionProps {
  anvilGroup: AnvilGroup
  onLabelAction: (state: LabelActionState) => void
  pendingLabels: Record<string, boolean>
}

function AnvilSection({ anvilGroup, onLabelAction, pendingLabels }: AnvilSectionProps) {
  const { t } = useTranslation('forge')
  const [open, setOpen] = useState(true)

  return (
    <div className="border-b border-gray-700/40 last:border-0">
      <button
        type="button"
        onClick={() => setOpen(prev => !prev)}
        aria-expanded={open}
        aria-label={t('queue.toggleAnvil', { anvil: anvilGroup.anvil })}
        className="w-full flex items-center gap-2 px-5 py-3 text-left hover:bg-gray-700/30 transition-colors min-h-[44px]"
      >
        {open ? (
          <ChevronDown size={14} className="text-gray-500 shrink-0" />
        ) : (
          <ChevronRight size={14} className="text-gray-500 shrink-0" />
        )}
        <span className="text-sm font-medium text-gray-300 truncate">{anvilGroup.anvil}</span>
        <span className="ml-auto flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-cyan-500/20 text-cyan-400 text-xs font-medium shrink-0">
          {anvilGroup.totalBeads}
        </span>
      </button>

      {open && (
        <div className="pb-2">
          {anvilGroup.sections.map(sg => (
            <div key={sg.section} className="px-5 pt-2">
              <div className="flex items-center gap-2 mb-1">
                <SectionBadge section={sg.section} />
                <span className="text-xs text-gray-600">{sg.beads.length}</span>
              </div>
              <ul className="pl-1">
                {sg.beads.map(bead => (
                  <BeadRow
                    key={bead.bead_id}
                    bead={bead}
                    onLabelAction={onLabelAction}
                    pendingLabels={pendingLabels}
                  />
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

interface FullQueueCardProps {
  showToast: (message: string, type: 'success' | 'error') => void
}

export default function FullQueueCard({ showToast }: FullQueueCardProps) {
  const { t } = useTranslation('forge')
  const [beads, setBeads] = useState<QueueBead[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [confirmAction, setConfirmAction] = useState<LabelActionState | null>(null)
  const [pendingLabels, setPendingLabels] = useState<Record<string, boolean>>({})

  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    let currentController: AbortController | null = null

    async function poll() {
      currentController = new AbortController()
      let stopPolling = false
      try {
        const res = await fetch('/api/forge/queue/all', {
          credentials: 'include',
          signal: currentController.signal,
        })
        if (cancelled) return
        if (res.status === 404) {
          if (!cancelled) {
            setBeads([])
            setError(t('fullQueue.queueUnavailable'))
          }
          stopPolling = true
          return
        }
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          if (!cancelled) {
            setError((data as { error?: string }).error ?? `HTTP ${res.status}`)
          }
        } else {
          const data: QueueBead[] = await res.json()
          if (!cancelled) {
            setBeads(data)
            setError(null)
          }
        }
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
        if (!cancelled) {
          setError(err instanceof Error ? err.message : t('unknownError'))
        }
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
      currentController?.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [t])

  async function applyLabelAction(action: LabelActionState) {
    setConfirmAction(null)
    const key = `${action.beadId}:${action.label}`
    setPendingLabels(prev => ({ ...prev, [key]: true }))
    try {
      let res: Response
      if (action.action === 'add') {
        res = await fetch(`/api/forge/beads/${encodeURIComponent(action.beadId)}/labels`, {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ label: action.label }),
        })
      } else {
        res = await fetch(
          `/api/forge/beads/${encodeURIComponent(action.beadId)}/labels/${encodeURIComponent(action.label)}`,
          { method: 'DELETE', credentials: 'include' }
        )
      }
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        const msgKey = action.action === 'add' ? 'fullQueue.labelAdded' : 'fullQueue.labelRemoved'
        showToast(t(msgKey, { label: action.label, id: action.beadId }), 'success')
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setPendingLabels(prev => {
        const next = { ...prev }
        delete next[key]
        return next
      })
    }
  }

  const anvilGroups = groupByAnvilAndSection(beads)
  const totalBeads = beads.length

  const confirmMsgKey =
    confirmAction?.action === 'add' ? 'fullQueue.addLabelConfirmMessage' : 'fullQueue.removeLabelConfirmMessage'
  const confirmTitleKey =
    confirmAction?.action === 'add' ? 'fullQueue.addLabelConfirmTitle' : 'fullQueue.removeLabelConfirmTitle'

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-gray-700/50">
        <ListOrdered size={18} className="text-cyan-400 shrink-0" />
        <h2 className="text-sm font-medium text-gray-300">{t('fullQueue.title')}</h2>
        {totalBeads > 0 && (
          <span className="ml-auto text-xs text-gray-500">
            {t('queue.totalBeads', { total: totalBeads })}
          </span>
        )}
      </div>

      {loading ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('fullQueue.loading')}</p>
      ) : error ? (
        <p className="px-5 py-6 text-sm text-red-400 text-center">{error}</p>
      ) : anvilGroups.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('queue.empty')}</p>
      ) : (
        <div>
          {anvilGroups.map(ag => (
            <AnvilSection
              key={ag.anvil}
              anvilGroup={ag}
              onLabelAction={setConfirmAction}
              pendingLabels={pendingLabels}
            />
          ))}
        </div>
      )}

      <ConfirmDialog
        open={confirmAction !== null}
        title={t(confirmTitleKey, { label: confirmAction?.label ?? '', id: confirmAction?.beadId ?? '' })}
        message={t(confirmMsgKey, {
          label: confirmAction?.label ?? '',
          id: confirmAction?.beadId ?? '',
        })}
        confirmLabel={
          confirmAction?.action === 'add' ? t('fullQueue.addLabel') : t('fullQueue.removeLabel')
        }
        onConfirm={() => { if (confirmAction) void applyLabelAction(confirmAction) }}
        onCancel={() => setConfirmAction(null)}
      />
    </div>
  )
}
