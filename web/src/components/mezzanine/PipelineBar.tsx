import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowRight } from 'lucide-react'
import type { WorkerInfo, OpenPR } from '../../hooks/useForgeStatus'
import PipelineStage from './PipelineStage'
import type { StageKey } from './PipelineStage'
import type { PipelineBeadInfo } from './PipelineBeadCard'

const STAGES: StageKey[] = ['queue', 'schematic', 'smith', 'temper', 'warden', 'pr', 'merged']

/** Map a worker's phase string to the corresponding pipeline stage. */
function phaseToStage(phase: string): StageKey {
  switch (phase) {
    case 'schematic':
      return 'schematic'
    case 'impl':
    case 'smith':
      return 'smith'
    case 'temper':
      return 'temper'
    case 'warden':
      return 'warden'
    case 'rebase':
    case 'bellows':
    case 'burnish':
      return 'pr'
    default:
      return 'smith'
  }
}

function workerCardStatus(w: WorkerInfo): 'active' | 'done' | 'failed' {
  if (w.status === 'failed' || w.status === 'cancelled') return 'failed'
  if (w.status === 'done') return 'done'
  return 'active'
}

interface QueueBead {
  bead_id: string
  title: string
  section: string
}

interface PipelineBarProps {
  workers: WorkerInfo[]
  openPRs?: OpenPR[]
  queueBeads?: QueueBead[]
  onBeadClick?: (beadId: string) => void
  onMerge?: (prId: number, prNumber: number) => void
  showToast?: (message: string, type: 'success' | 'error') => void
  onActionComplete?: () => void
}

export default function PipelineBar({ workers, openPRs, queueBeads, onBeadClick, onMerge, showToast, onActionComplete }: PipelineBarProps) {
  const { t } = useTranslation('forge')

  const stageBeads = useMemo(() => {
    const map = new Map<StageKey, PipelineBeadInfo[]>()
    for (const s of STAGES) map.set(s, [])

    // Track which bead IDs are already placed (avoid duplicates)
    const placed = new Set<string>()

    // Active workers go to their phase stage
    for (const w of workers) {
      if (w.status !== 'pending' && w.status !== 'running') continue
      const stage = phaseToStage(w.phase)
      map.get(stage)!.push({
        beadId: w.bead_id,
        title: w.title,
        status: workerCardStatus(w),
        anvil: w.anvil,
      })
      placed.add(w.bead_id)
    }

    // Open PRs that aren't actively being worked on go to 'pr' stage
    if (openPRs) {
      for (const pr of openPRs) {
        if (placed.has(pr.bead_id)) continue
        map.get('pr')!.push({
          beadId: pr.bead_id,
          title: pr.title,
          status: pr.ci_passing && pr.has_approval ? 'done' : 'active',
          anvil: pr.anvil,
          pr: {
            prId: pr.id,
            prNumber: pr.number,
            ciPassing: pr.ci_passing,
            ciPending: pr.ci_pending,
            hasApproval: pr.has_approval,
            changesRequested: pr.changes_requested,
            isConflicting: pr.is_conflicting,
            hasUnresolvedThreads: pr.has_unresolved_threads,
            hasPendingReviews: pr.has_pending_reviews,
            bellowsManaged: pr.bellows_managed,
          },
        })
        placed.add(pr.bead_id)
      }
    }

    // Queue beads in 'ready' section go to the queue stage
    if (queueBeads) {
      for (const b of queueBeads) {
        if (placed.has(b.bead_id)) continue
        if (b.section !== 'ready') continue
        map.get('queue')!.push({
          beadId: b.bead_id,
          title: b.title,
          status: 'active',
        })
        placed.add(b.bead_id)
      }
    }

    return map
  }, [workers, openPRs, queueBeads])

  return (
    <section aria-label={t('mezzanine.pipeline.title')}>
      <h2 className="text-sm font-medium text-gray-300 mb-2">
        {t('mezzanine.pipeline.title')}
      </h2>
      <div className="flex items-stretch gap-1 overflow-x-auto pb-1">
        {STAGES.map((stage, i) => (
          <div key={stage} className="flex items-stretch min-w-0">
            {i > 0 && (
              <div className="flex items-center px-0.5 text-gray-600 shrink-0" aria-hidden="true">
                <ArrowRight size={14} />
              </div>
            )}
            <PipelineStage
              stage={stage}
              beads={stageBeads.get(stage) ?? []}
              onBeadClick={onBeadClick}
              onMerge={onMerge}
              showToast={showToast}
              onActionComplete={onActionComplete}
            />
          </div>
        ))}
      </div>
    </section>
  )
}
