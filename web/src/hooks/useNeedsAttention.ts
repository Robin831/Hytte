import { useMemo } from 'react'
import { useForgeStatus } from './useForgeStatus'
import type { StuckBead, OpenPR } from './useForgeStatus'

/** Source of the stuck item: retries table or an exhausted PR. */
export type NeedsAttentionSource = 'retry' | 'pr'

/** Why this item is stuck. */
export type NeedsAttentionStatus =
  | 'needs_human'
  | 'clarification_needed'
  | 'dispatch_failure'
  | 'ci_exhausted'
  | 'review_exhausted'
  | 'max_retries'

/** Actions that can be taken on this item. */
export type NeedsAttentionAction = 'retry' | 'approve' | 'dismiss' | 'forceSmith'

export interface NeedsAttentionItem {
  /** Bead identifier. */
  beadId: string
  /** Anvil (repo) this item belongs to. */
  anvil: string
  /** Whether the item originated from retries or a stuck PR. */
  source: NeedsAttentionSource
  /** Primary reason this item needs attention. */
  status: NeedsAttentionStatus
  /** All applicable statuses (an item can match multiple). */
  statuses: NeedsAttentionStatus[]
  /** Actions available for this item. */
  actions: NeedsAttentionAction[]
  /** Number of retry attempts so far. */
  retryCount: number
  /** Last error message, if any. */
  lastError: string
  /** When this item was last updated (ISO string). */
  updatedAt: string
  /** Number of dispatch failures. */
  dispatchFailures: number
  /** Associated PR, if one exists. */
  pr?: OpenPR
  /** The raw stuck bead data from the backend. */
  raw: StuckBead
}

function classifyStatuses(bead: StuckBead, pr: OpenPR | undefined): NeedsAttentionStatus[] {
  const statuses: NeedsAttentionStatus[] = []

  if (bead.clarification_needed) statuses.push('clarification_needed')
  if (bead.needs_human) statuses.push('needs_human')
  if (bead.dispatch_failures > 0) statuses.push('dispatch_failure')

  if (pr) {
    if (pr.ci_fix_count > 0) statuses.push('ci_exhausted')
    if (pr.review_fix_count > 0) statuses.push('review_exhausted')
  }

  if (bead.retry_count > 0 && statuses.length === 0) statuses.push('max_retries')

  return statuses
}

function classifySource(bead: StuckBead): NeedsAttentionSource {
  // Items from the retries table have needs_human, clarification_needed, or
  // dispatch_failures set. PR-only stuck items typically only have retry_count
  // from the synthetic Retry row the backend creates for stuck PRs — but they
  // won't have needs_human or clarification_needed set.
  if (bead.needs_human || bead.clarification_needed || bead.dispatch_failures > 0) {
    return 'retry'
  }
  return 'pr'
}

function availableActions(source: NeedsAttentionSource, statuses: NeedsAttentionStatus[]): NeedsAttentionAction[] {
  const actions: NeedsAttentionAction[] = ['retry', 'dismiss']

  // Approve is useful when the item just needs a human sign-off
  if (statuses.includes('needs_human') || statuses.includes('ci_exhausted') || statuses.includes('review_exhausted')) {
    actions.push('approve')
  }

  // Force smith lets a human re-run with extra context
  if (source === 'retry' || statuses.includes('clarification_needed')) {
    actions.push('forceSmith')
  }

  return actions
}

function buildItem(bead: StuckBead, pr: OpenPR | undefined): NeedsAttentionItem {
  const source = classifySource(bead)
  const statuses = classifyStatuses(bead, pr)
  const actions = availableActions(source, statuses)

  return {
    beadId: bead.bead_id,
    anvil: bead.anvil,
    source,
    status: statuses[0] ?? 'needs_human',
    statuses,
    actions,
    retryCount: bead.retry_count,
    lastError: bead.last_error,
    updatedAt: bead.updated_at,
    dispatchFailures: bead.dispatch_failures,
    pr,
    raw: bead,
  }
}

/**
 * Combines stuck retries and exhausted PRs from forge status into a unified
 * list of items that need human attention. This is the foundational data layer
 * consumed by the status bar badge and needs-attention modal.
 */
export function useNeedsAttention(): {
  items: NeedsAttentionItem[]
  count: number
  isLoading: boolean
} {
  const { status, loading } = useForgeStatus()

  const items = useMemo(() => {
    if (!status) return []

    const prByBeadId = new Map<string, OpenPR>()
    if (status.open_prs) {
      for (const pr of status.open_prs) {
        if (!prByBeadId.has(pr.bead_id)) {
          prByBeadId.set(pr.bead_id, pr)
        }
      }
    }

    return (status.stuck ?? []).map(bead => buildItem(bead, prByBeadId.get(bead.bead_id)))
  }, [status])

  return {
    items,
    count: items.length,
    isLoading: loading,
  }
}
