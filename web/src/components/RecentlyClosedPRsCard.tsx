import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { GitPullRequestClosed, ExternalLink, ChevronDown, ChevronRight } from 'lucide-react'
import { CollapsiblePanelHeader } from './CollapsiblePanelHeader'
import { usePanelCollapse } from '../hooks/usePanelCollapse'
import { formatDateTime } from '../utils/formatDate'

interface ClosedPR {
  id: number
  number: number
  title: string
  anvil: string
  bead_id: string
  status: string
  last_checked?: string
}

interface AnvilGroup {
  anvil: string
  prs: ClosedPR[]
}

function groupByAnvil(prs: ClosedPR[]): AnvilGroup[] {
  const map = new Map<string, ClosedPR[]>()
  for (const pr of prs) {
    const list = map.get(pr.anvil) ?? []
    list.push(pr)
    map.set(pr.anvil, list)
  }
  return Array.from(map.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([anvil, prs]) => ({ anvil, prs }))
}

interface RecentlyClosedPRsCardProps {
  onBeadClick?: (beadId: string) => void
}

export default function RecentlyClosedPRsCard({ onBeadClick }: RecentlyClosedPRsCardProps) {
  const { t } = useTranslation('forge')
  const [prs, setPrs] = useState<ClosedPR[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [isOpen, toggle] = usePanelCollapse('closed-prs', false)
  const [expandedAnvils, setExpandedAnvils] = useState<Set<string>>(new Set())

  useEffect(() => {
    let cancelled = false
    async function fetchClosedPRs() {
      try {
        const res = await fetch('/api/forge/prs/closed', { credentials: 'include' })
        if (cancelled) return
        if (res.ok) {
          const data: ClosedPR[] = await res.json()
          if (!cancelled) setPrs(data)
        } else {
          if (!cancelled) setError(true)
        }
      } catch {
        if (!cancelled) setError(true)
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void fetchClosedPRs()
    return () => { cancelled = true }
  }, [])

  const groups = useMemo(() => groupByAnvil(prs), [prs])

  function toggleAnvil(anvil: string) {
    setExpandedAnvils(prev => {
      const next = new Set(prev)
      if (next.has(anvil)) {
        next.delete(anvil)
      } else {
        next.add(anvil)
      }
      return next
    })
  }

  function fmtDate(dateStr: string | undefined): string {
    if (!dateStr) return ''
    try {
      return formatDateTime(dateStr, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <CollapsiblePanelHeader
        isOpen={isOpen}
        toggle={toggle}
        panelId="closed-prs-panel"
        icon={<GitPullRequestClosed size={18} className="text-gray-500 shrink-0" />}
        title={t('closedPRs.title')}
        trailing={
          prs.length > 0 ? (
            <span className="flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-gray-700 text-gray-400 text-xs font-medium">
              {prs.length}
            </span>
          ) : undefined
        }
      />

      <div id="closed-prs-panel" hidden={!isOpen}>
        {loading ? (
          <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('closedPRs.loading')}</p>
        ) : error ? (
          <p className="px-5 py-6 text-sm text-red-400 text-center">{t('closedPRs.error')}</p>
        ) : prs.length === 0 ? (
          <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('closedPRs.empty')}</p>
        ) : (
          <div className="divide-y divide-gray-700/40">
            {groups.map(group => {
              const isExpanded = groups.length === 1 || expandedAnvils.has(group.anvil)
              return (
                <div key={group.anvil}>
                  {groups.length > 1 && (
                    <button
                      type="button"
                      onClick={() => toggleAnvil(group.anvil)}
                      className="w-full flex items-center gap-2 px-5 py-2.5 text-left hover:bg-gray-700/20 transition-colors"
                      aria-expanded={isExpanded}
                    >
                      {isExpanded ? (
                        <ChevronDown size={14} className="text-gray-500 shrink-0" />
                      ) : (
                        <ChevronRight size={14} className="text-gray-500 shrink-0" />
                      )}
                      <span className="text-xs font-medium text-gray-400">{group.anvil}</span>
                      <span className="text-xs text-gray-600">{group.prs.length}</span>
                    </button>
                  )}
                  {isExpanded && (
                    <div className="divide-y divide-gray-700/20">
                      {group.prs.map(pr => {
                        const githubUrl = pr.anvil.includes('/')
                          ? `https://github.com/${pr.anvil}/pull/${pr.number}`
                          : null
                        return (
                          <div key={pr.id} className="px-5 py-3 flex items-center gap-3">
                            <div className="flex flex-col gap-0.5 min-w-0 flex-1">
                              <span className="text-sm text-gray-300 truncate">{pr.title}</span>
                              <div className="flex items-center gap-2 flex-wrap">
                                <span className="text-xs text-gray-500">#{pr.number}</span>
                                {pr.bead_id && (
                                  <button
                                    type="button"
                                    onClick={() => onBeadClick?.(pr.bead_id)}
                                    className="text-xs text-blue-400 hover:text-blue-300 transition-colors"
                                  >
                                    {pr.bead_id}
                                  </button>
                                )}
                                <span className={`text-xs ${pr.status === 'merged' ? 'text-purple-400' : 'text-gray-500'}`}>
                                  {pr.status === 'merged' ? t('closedPRs.merged') : t('closedPRs.closed')}
                                </span>
                                {pr.last_checked && (
                                  <span className="text-xs text-gray-600">{fmtDate(pr.last_checked)}</span>
                                )}
                              </div>
                            </div>
                            {githubUrl && (
                              <a
                                href={githubUrl}
                                target="_blank"
                                rel="noopener noreferrer"
                                aria-label={t('readyToMerge.viewOnGitHub')}
                                className="flex items-center justify-center min-h-[32px] min-w-[32px] rounded-lg text-gray-500 hover:text-gray-300 hover:bg-gray-700/50 transition-colors shrink-0"
                              >
                                <ExternalLink size={14} />
                              </a>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
