import { useMemo } from 'react'
import { useForgeStatus } from './useForgeStatus'
import type { StuckBead, OpenPR, ForgeStatus } from './useForgeStatus'

/** Source of the stuck item: retries table or an exhausted PR. */
export type NeedsAttentionSource = 'retry' | 'pr'

/** Why this item is stuck. */
export type NeedsAttentionStatus =
  | 'needs_human'
  | 'clarification_needed'
  | 'dispatch_failure'
  | 'ci_exhausted'
  | 'review_exhausted'

/** Actions that can be taken on this item. */
export type NeedsAttentionAction = 'retry' | 'approve' | 'dismiss' | 'forceSmith' | 'wardenRerun'

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

export function hasExhaustedReason(lastError: string | undefined, kind: 'ci' | 'review'): boolean {
  if (!lastError) return false

  const normalized = lastError.toLowerCase()
  const patterns =
    kind === 'ci'
      ? [
          /\bci_exhausted\b/,
          /\bci exhausted\b/,
          /\bexhausted ci\b/,
          /\bci\b.*\bexhausted\b/,
          /\bexhausted\b.*\bci\b/,
        ]
      : [
          /\breview_exhausted\b/,
          /\breview exhausted\b/,
          /\bexhausted review\b/,
          /\breview\b.*\bexhausted\b/,
          /\bexhausted\b.*\breview\b/,
        ]

  return patterns.some((pattern) => pattern.test(normalized))
}

export function isPRExhaustion(bead: StuckBead): boolean {
  const lastError = bead.last_error?.toLowerCase() ?? ''

  return (
    lastError.includes('ci_exhausted') ||
    lastError.includes('ci exhausted') ||
    lastError.includes('review_exhausted') ||
    lastError.includes('review exhausted') ||
    lastError.includes('max_retries') ||
    lastError.includes('max retries')
  )
}

export function classifyStatuses(bead: StuckBead, _pr: OpenPR | undefined): NeedsAttentionStatus[] {
  const statuses: NeedsAttentionStatus[] = []

  if (bead.clarification_needed) statuses.push('clarification_needed')
  if (bead.needs_human) statuses.push('needs_human')
  if (bead.dispatch_failures > 0) statuses.push('dispatch_failure')

  if (hasExhaustedReason(bead.last_error, 'ci')) statuses.push('ci_exhausted')
  if (hasExhaustedReason(bead.last_error, 'review')) statuses.push('review_exhausted')

  return statuses
}

export function classifySource(bead: StuckBead): NeedsAttentionSource {
  // Stuck PRs are merged into status.stuck as synthetic retry rows and may
  // still have needs_human set, so do not use needs_human to distinguish
  // source. Instead, detect the known PR exhaustion reasons from last_error
  // and fall back to retry-table signals for non-PR items.
  if (isPRExhaustion(bead)) {
    return 'pr'
  }

  if (bead.clarification_needed || bead.dispatch_failures > 0 || bead.needs_human) {
    return 'retry'
  }

  return 'retry'
}

export function availableActions(source: NeedsAttentionSource, statuses: NeedsAttentionStatus[]): NeedsAttentionAction[] {
  const actions: NeedsAttentionAction[] = ['dismiss']

  const hasRetryEntry = source === 'retry'

  // Retry is only safe for items backed by a real retries-table row.
  if (hasRetryEntry) {
    actions.push('retry')
  }

  // Approve is useful when the item just needs a human sign-off, but bead
  // action endpoints require a real retries-table entry and will 404 for
  // synthetic PR-stuck items.
  if (
    hasRetryEntry &&
    (statuses.includes('needs_human') || statuses.includes('ci_exhausted') || statuses.includes('review_exhausted'))
  ) {
    actions.push('approve')
  }

  // Force smith lets a human re-run with extra context, but only for items
  // backed by a retries-table row.
  if (hasRetryEntry && statuses.includes('clarification_needed')) {
    actions.push('forceSmith')
  }

  // Warden rerun is useful when the warden rejected changes that are actually
  // fine — re-runs the warden review without restarting the smith.
  if (
    hasRetryEntry &&
    (statuses.includes('needs_human') || statuses.includes('review_exhausted'))
  ) {
    actions.push('wardenRerun')
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

/** Returns true when a stuck bead meets the criteria to appear in the Needs Attention panel. */
function meetsAttentionCriteria(bead: StuckBead): boolean {
  return bead.needs_human || bead.clarification_needed || bead.dispatch_failures > 0 || bead.retry_count > 0
}

/**
 * Pure helper: computes the list of needs-attention items from a forge status
 * snapshot. Consumers that already hold a `ForgeStatus` value (e.g. `StatusBar`)
 * can call this directly instead of invoking `useNeedsAttention()`, avoiding a
 * second `useForgeStatus()` polling loop.
 */
export function computeNeedsAttentionItems(status: ForgeStatus | null | undefined): NeedsAttentionItem[] {
  if (!status) return []

  const prByBeadId = new Map<string, OpenPR>()
  if (status.open_prs) {
    for (const pr of status.open_prs) {
      if (!prByBeadId.has(pr.bead_id)) {
        prByBeadId.set(pr.bead_id, pr)
      }
    }
  }

  return (status.stuck ?? [])
    .filter(meetsAttentionCriteria)
    .map(bead => buildItem(bead, prByBeadId.get(bead.bead_id)))
}

/**
 * Combines stuck retries and exhausted PRs from forge status into a unified
 * list of items that need human attention. This is the foundational data layer
 * consumed by the status bar badge and needs-attention panel.
 */
export function useNeedsAttention(): {
  items: NeedsAttentionItem[]
  count: number
  isLoading: boolean
} {
  const { status, loading } = useForgeStatus()
  const items = useMemo(() => computeNeedsAttentionItems(status), [status])
  return {
    items,
    count: items.length,
    isLoading: loading,
  }
}
