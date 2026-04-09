import { describe, it, expect } from 'vitest'
import {
  hasExhaustedReason,
  isPRExhaustion,
  classifyStatuses,
  classifySource,
  availableActions,
} from './useNeedsAttention'
import type { StuckBead, OpenPR } from './useForgeStatus'

function makeStuckBead(overrides: Partial<StuckBead> = {}): StuckBead {
  return {
    bead_id: 'test-bead',
    anvil: 'owner/repo',
    retry_count: 0,
    needs_human: false,
    clarification_needed: false,
    last_error: '',
    updated_at: '2026-01-01T00:00:00Z',
    dispatch_failures: 0,
    ...overrides,
  }
}

function makeOpenPR(overrides: Partial<OpenPR> = {}): OpenPR {
  return {
    id: 1,
    number: 42,
    title: 'Test PR',
    anvil: 'owner/repo',
    bead_id: 'test-bead',
    branch: 'forge/test',
    ci_passing: true,
    ci_pending: false,
    has_approval: false,
    changes_requested: false,
    is_conflicting: false,
    has_unresolved_threads: false,
    has_pending_reviews: false,
    bellows_managed: true,
    ci_fix_count: 0,
    review_fix_count: 0,
    ...overrides,
  }
}

describe('hasExhaustedReason', () => {
  it('returns false for undefined', () => {
    expect(hasExhaustedReason(undefined, 'ci')).toBe(false)
    expect(hasExhaustedReason(undefined, 'review')).toBe(false)
  })

  it('returns false for empty string', () => {
    expect(hasExhaustedReason('', 'ci')).toBe(false)
    expect(hasExhaustedReason('', 'review')).toBe(false)
  })

  it('detects ci_exhausted keyword', () => {
    expect(hasExhaustedReason('ci_exhausted after 3 attempts', 'ci')).toBe(true)
  })

  it('detects "ci exhausted" (space variant)', () => {
    expect(hasExhaustedReason('CI exhausted', 'ci')).toBe(true)
  })

  it('detects review_exhausted keyword', () => {
    expect(hasExhaustedReason('review_exhausted', 'review')).toBe(true)
  })

  it('detects "review exhausted" (space variant)', () => {
    expect(hasExhaustedReason('Review Exhausted after feedback', 'review')).toBe(true)
  })

  it('does not cross-detect ci vs review', () => {
    expect(hasExhaustedReason('ci_exhausted', 'review')).toBe(false)
    expect(hasExhaustedReason('review_exhausted', 'ci')).toBe(false)
  })

  it('handles mixed-case input', () => {
    expect(hasExhaustedReason('CI_EXHAUSTED', 'ci')).toBe(true)
    expect(hasExhaustedReason('REVIEW_EXHAUSTED', 'review')).toBe(true)
  })
})

describe('isPRExhaustion', () => {
  it('returns false for a clean retry bead', () => {
    expect(isPRExhaustion(makeStuckBead({ needs_human: true }))).toBe(false)
  })

  it('detects ci_exhausted in last_error', () => {
    expect(isPRExhaustion(makeStuckBead({ last_error: 'ci_exhausted' }))).toBe(true)
  })

  it('detects review_exhausted in last_error', () => {
    expect(isPRExhaustion(makeStuckBead({ last_error: 'review_exhausted' }))).toBe(true)
  })

  it('detects max_retries in last_error', () => {
    expect(isPRExhaustion(makeStuckBead({ last_error: 'max_retries reached' }))).toBe(true)
  })

  it('is case-insensitive', () => {
    expect(isPRExhaustion(makeStuckBead({ last_error: 'CI_EXHAUSTED' }))).toBe(true)
  })
})

describe('classifyStatuses', () => {
  it('returns empty array for a clean bead', () => {
    expect(classifyStatuses(makeStuckBead(), undefined)).toEqual([])
  })

  it('adds needs_human', () => {
    expect(classifyStatuses(makeStuckBead({ needs_human: true }), undefined)).toContain('needs_human')
  })

  it('adds clarification_needed', () => {
    expect(classifyStatuses(makeStuckBead({ clarification_needed: true }), undefined)).toContain('clarification_needed')
  })

  it('adds dispatch_failure when dispatch_failures > 0', () => {
    expect(classifyStatuses(makeStuckBead({ dispatch_failures: 2 }), undefined)).toContain('dispatch_failure')
  })

  it('adds ci_exhausted from last_error, not from PR ci_fix_count', () => {
    // PR has non-zero ci_fix_count — must NOT trigger ci_exhausted
    const pr = makeOpenPR({ ci_fix_count: 3 })
    const statuses = classifyStatuses(makeStuckBead({ needs_human: true }), pr)
    expect(statuses).not.toContain('ci_exhausted')

    // last_error has ci_exhausted — MUST trigger
    const beadWithError = makeStuckBead({ last_error: 'ci_exhausted' })
    expect(classifyStatuses(beadWithError, undefined)).toContain('ci_exhausted')
  })

  it('adds review_exhausted from last_error, not from PR review_fix_count', () => {
    const pr = makeOpenPR({ review_fix_count: 5 })
    const statuses = classifyStatuses(makeStuckBead({ needs_human: true }), pr)
    expect(statuses).not.toContain('review_exhausted')

    const beadWithError = makeStuckBead({ last_error: 'review_exhausted' })
    expect(classifyStatuses(beadWithError, undefined)).toContain('review_exhausted')
  })

  it('can return multiple statuses', () => {
    const bead = makeStuckBead({ needs_human: true, clarification_needed: true, dispatch_failures: 1 })
    const statuses = classifyStatuses(bead, undefined)
    expect(statuses).toContain('needs_human')
    expect(statuses).toContain('clarification_needed')
    expect(statuses).toContain('dispatch_failure')
  })
})

describe('classifySource', () => {
  it('returns "retry" for a needs_human bead without exhaustion error', () => {
    expect(classifySource(makeStuckBead({ needs_human: true }))).toBe('retry')
  })

  it('returns "retry" for a clarification_needed bead', () => {
    expect(classifySource(makeStuckBead({ clarification_needed: true }))).toBe('retry')
  })

  it('returns "retry" for a dispatch-failure bead', () => {
    expect(classifySource(makeStuckBead({ dispatch_failures: 1 }))).toBe('retry')
  })

  it('returns "pr" when last_error indicates ci_exhausted (even if needs_human is set)', () => {
    // Synthetic PR-stuck rows set needs_human: true — classifySource must still return 'pr'
    const bead = makeStuckBead({ needs_human: true, last_error: 'ci_exhausted' })
    expect(classifySource(bead)).toBe('pr')
  })

  it('returns "pr" when last_error indicates review_exhausted', () => {
    expect(classifySource(makeStuckBead({ last_error: 'review_exhausted' }))).toBe('pr')
  })

  it('returns "pr" when last_error indicates max_retries', () => {
    expect(classifySource(makeStuckBead({ last_error: 'max_retries reached' }))).toBe('pr')
  })
})

describe('availableActions', () => {
  it('always includes dismiss', () => {
    expect(availableActions('retry', [])).toContain('dismiss')
    expect(availableActions('pr', [])).toContain('dismiss')
  })

  it('includes retry for source=retry, not for source=pr', () => {
    expect(availableActions('retry', [])).toContain('retry')
    expect(availableActions('pr', [])).not.toContain('retry')
  })

  it('includes approve for retry source with needs_human', () => {
    expect(availableActions('retry', ['needs_human'])).toContain('approve')
  })

  it('does NOT include approve for pr source even with needs_human', () => {
    expect(availableActions('pr', ['needs_human'])).not.toContain('approve')
  })

  it('includes approve for retry source with ci_exhausted', () => {
    expect(availableActions('retry', ['ci_exhausted'])).toContain('approve')
  })

  it('does NOT include approve for pr source with ci_exhausted', () => {
    expect(availableActions('pr', ['ci_exhausted'])).not.toContain('approve')
  })

  it('includes forceSmith for retry source with clarification_needed', () => {
    expect(availableActions('retry', ['clarification_needed'])).toContain('forceSmith')
  })

  it('does NOT include forceSmith for pr source', () => {
    expect(availableActions('pr', ['clarification_needed'])).not.toContain('forceSmith')
  })

  it('does NOT include forceSmith for retry source without clarification_needed', () => {
    expect(availableActions('retry', ['needs_human'])).not.toContain('forceSmith')
  })

  it('includes wardenRerun for retry source with needs_human', () => {
    expect(availableActions('retry', ['needs_human'])).toContain('wardenRerun')
  })

  it('includes wardenRerun for retry source with review_exhausted', () => {
    expect(availableActions('retry', ['review_exhausted'])).toContain('wardenRerun')
  })

  it('does NOT include wardenRerun for pr source', () => {
    expect(availableActions('pr', ['needs_human'])).not.toContain('wardenRerun')
    expect(availableActions('pr', ['review_exhausted'])).not.toContain('wardenRerun')
  })

  it('does NOT include wardenRerun for retry source without needs_human or review_exhausted', () => {
    expect(availableActions('retry', ['clarification_needed'])).not.toContain('wardenRerun')
    expect(availableActions('retry', ['ci_exhausted'])).not.toContain('wardenRerun')
    expect(availableActions('retry', ['dispatch_failure'])).not.toContain('wardenRerun')
  })
})
