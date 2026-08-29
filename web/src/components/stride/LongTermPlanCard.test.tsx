// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { LongTermPlanCard, EXTENSION_LEAD_WEEKS } from './LongTermPlanCard'
import enStride from '../../../public/locales/en/stride.json'
import type { MacroPlanView, MacroWeek, WeekSummary } from '../../types/stride'

// ── Translation mock ──────────────────────────────────────────────────────────
// Resolves against the real English bundle so a missing key fails the test
// rather than silently rendering the key name.

type JsonValue = string | number | boolean | null | JsonObject | JsonValue[]
interface JsonObject { [key: string]: JsonValue }

function resolveKey(obj: JsonObject, parts: string[]): JsonValue | undefined {
  const [head, ...rest] = parts
  const val = obj[head]
  if (rest.length === 0) return val
  if (val && typeof val === 'object' && !Array.isArray(val)) {
    return resolveKey(val as JsonObject, rest)
  }
  return undefined
}

function interpolate(template: string, opts: Record<string, unknown>): string {
  return template.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
}

function t(key: string, opts?: Record<string, unknown>): string {
  const bundle = enStride as unknown as JsonObject
  if (opts?.count !== undefined) {
    const plural = resolveKey(bundle, `${key}${Number(opts.count) === 1 ? '_one' : '_other'}`.split('.'))
    if (typeof plural === 'string') return interpolate(plural, opts)
  }
  const val = resolveKey(bundle, key.split('.'))
  if (typeof val === 'string') return opts ? interpolate(val, opts) : val
  if (typeof opts?.defaultValue === 'string') return interpolate(opts.defaultValue, opts)
  return key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('lucide-react', async () => (await import('../../test/lucideStub')).lucideStub)

vi.mock('../../utils/formatDate', () => ({
  formatDate: (date: Date | string, options?: Intl.DateTimeFormatOptions) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return d.toLocaleDateString('en', options)
  },
  formatNumber: (n: number, options?: Intl.NumberFormatOptions) => n.toLocaleString('en', options),
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

// Monday of the week `offset` weeks from the current one, so the block always
// straddles "today" regardless of when the suite runs.
function mondayOffset(offset: number): string {
  const d = new Date()
  d.setHours(0, 0, 0, 0)
  d.setDate(d.getDate() - ((d.getDay() + 6) % 7) + offset * 7)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

// Four weeks around today: two finished, the one in progress, and one ahead.
const WEEK_SPECS: Array<Partial<MacroWeek> & { offset: number }> = [
  { offset: -2, seq: 1, phase: 'base', status: 'materialised', target_km: 50 },
  { offset: -1, seq: 2, phase: 'base', status: 'materialised', target_km: 50 },
  { offset: 0, seq: 3, phase: 'build', status: 'materialised', target_km: 50 },
  { offset: 1, seq: 4, phase: 'build', status: 'planned', target_km: 60 },
]

const WEEKS: MacroWeek[] = WEEK_SPECS.map((spec, i) => ({
  id: i + 1,
  macro_plan_id: 7,
  user_id: 1,
  week_start: mondayOffset(spec.offset),
  seq: spec.seq ?? i + 1,
  phase: spec.phase ?? 'base',
  mesocycle: 'Aerobic base',
  load_level: 'normal',
  target_km: spec.target_km ?? 50,
  target_sessions: 5,
  race_id: null,
  key_sessions: [{ type: 'threshold', focus: '3 x 3 km', library_id: null }],
  intent: `Intent for week ${spec.seq ?? i + 1}`,
  status: spec.status ?? 'planned',
}))

function historyWeek(offset: number, meters: number, completed: number): WeekSummary {
  return {
    plan_id: offset + 100,
    week_start: mondayOffset(offset),
    week_end: mondayOffset(offset + 1),
    phase: 'base',
    sessions_planned: 5,
    sessions_completed: completed,
    completion_rate: (completed / 5) * 100,
    total_distance_meters: meters,
  }
}

// Under target, exactly on target, over target — one of each so the bar's three
// states are all exercised by the same render.
const HISTORY: WeekSummary[] = [
  historyWeek(-2, 42500, 4),
  historyWeek(-1, 50000, 5),
  historyWeek(0, 55000, 6),
]

// Overrides merge into `plan` rather than replacing it, so a test can change
// one column (stale_reason) without restating the whole block.
function macroView(overrides: Partial<MacroPlanView> = {}): MacroPlanView {
  const base: MacroPlanView = {
    plan: {
      id: 7,
      user_id: 1,
      start_week: WEEKS[0].week_start,
      end_week: WEEKS[WEEKS.length - 1].week_start,
      status: 'active',
      stale_reason: '',
      goal: {
        primary_focus: 'half_marathon',
        statement: 'Run 1:25 for the half marathon',
        target_hm_time_s: 5100,
        benchmark: '3 x 3 km at threshold',
        rationale: 'Threshold volume is the limiter.',
        anchor_race_id: null,
      },
      periodisation: [],
      model: 'test',
      generated_by: 'scheduled',
      previous_plan_id: null,
      created_at: '2026-01-01T00:00:00Z',
    },
    weeks: WEEKS,
    current_goal_revision: null,
    revisions: [],
    has_next_block: false,
  }
  return { ...base, ...overrides, plan: { ...base.plan, ...overrides.plan } }
}

function renderCard(props: Partial<Parameters<typeof LongTermPlanCard>[0]> = {}) {
  return render(
    <LongTermPlanCard
      view={macroView()}
      historyWeeks={HISTORY}
      onRegenerate={() => {}}
      onExtend={() => {}}
      {...props}
    />,
  )
}

function expandWeeks() {
  fireEvent.click(screen.getByRole('button', { name: /Week by week/ }))
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('LongTermPlanCard – week list', () => {
  it('is collapsed by default and expands to one row per week', () => {
    renderCard()

    expect(screen.queryByText('Intent for week 1')).not.toBeInTheDocument()
    const toggle = screen.getByRole('button', { name: /Week by week/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getAllByRole('progressbar')).toHaveLength(3)
    for (const week of WEEKS) {
      expect(screen.getByText(`Intent for week ${week.seq}`)).toBeInTheDocument()
    }
  })

  it('shows the block week count in the collapsed header', () => {
    renderCard()

    expect(screen.getByRole('button', { name: /Week by week/ })).toHaveTextContent('4 weeks')
  })

  it('collapses again on a second click', () => {
    renderCard()

    expandWeeks()
    expect(screen.getByText('Intent for week 1')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /Week by week/ }))
    expect(screen.queryByText('Intent for week 1')).not.toBeInTheDocument()
  })
})

describe('LongTermPlanCard – target vs actual', () => {
  it('renders a partly filled bar when the week fell short of target', () => {
    renderCard()
    expandWeeks()

    const bar = screen.getByRole('progressbar', {
      name: 'Distance completed against target for week 1',
    })
    expect(bar).toHaveAttribute('aria-valuenow', '42.5')
    expect(bar).toHaveAttribute('aria-valuemax', '50')
    expect(bar).toHaveAttribute('aria-valuetext', '42.5 / 50 km')
    expect(bar.firstElementChild).toHaveStyle({ width: '85%' })
  })

  it('fills the bar exactly when actual equals target', () => {
    renderCard()
    expandWeeks()

    const bar = screen.getByRole('progressbar', {
      name: 'Distance completed against target for week 2',
    })
    expect(bar).toHaveAttribute('aria-valuetext', '50 / 50 km')
    expect(bar.firstElementChild).toHaveStyle({ width: '100%' })
  })

  it('clamps the bar at full width when actual overshoots the target', () => {
    renderCard()
    expandWeeks()

    const bar = screen.getByRole('progressbar', {
      name: 'Distance completed against target for week 3',
    })
    expect(bar).toHaveAttribute('aria-valuenow', '55')
    expect(bar.firstElementChild).toHaveStyle({ width: '100%' })
    // The label still states the overshoot the clamped bar cannot show.
    expect(screen.getByText('55 / 50 km')).toBeInTheDocument()
  })

  it('states the target alone for a week with no history yet', () => {
    renderCard()
    expandWeeks()

    expect(screen.getByText('60 km planned')).toBeInTheDocument()
    expect(screen.getByText('5 planned')).toBeInTheDocument()
    expect(
      screen.queryByRole('progressbar', { name: 'Distance completed against target for week 4' }),
    ).not.toBeInTheDocument()
  })

  it('draws no bar for a week with a zero target', () => {
    const view = macroView()
    view.weeks = [{ ...WEEKS[0], target_km: 0 }]
    render(<LongTermPlanCard view={view} historyWeeks={HISTORY} />)
    expandWeeks()

    expect(screen.getByText('42.5 / 0 km')).toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })

  it('shows completed against target sessions', () => {
    renderCard()
    expandWeeks()

    expect(screen.getByText('4 / 5')).toBeInTheDocument()
    expect(screen.getByText('6 / 5')).toBeInTheDocument()
  })

  it('renders the state badge of each week', () => {
    renderCard()
    expandWeeks()

    expect(screen.getAllByText('Materialised')).toHaveLength(3)
    expect(screen.getAllByText('Planned')).toHaveLength(1)
  })

  it('surfaces an actuals failure with a retry inside the list', () => {
    const onReloadHistory = vi.fn()
    renderCard({ historyWeeks: [], historyError: true, onReloadHistory })
    expandWeeks()

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load completed training.')
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(onReloadHistory).toHaveBeenCalledTimes(1)
  })

  it('says the actuals are still loading without hiding the targets', () => {
    renderCard({ historyWeeks: [], historyLoading: true })
    expandWeeks()

    expect(screen.getByText('Loading completed training…')).toBeInTheDocument()
    expect(screen.getByText('Intent for week 1')).toBeInTheDocument()
  })
})

describe('LongTermPlanCard – stale banner', () => {
  it('is absent for a fresh block', () => {
    renderCard()

    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('explains a race change and regenerates from its button', () => {
    const onRegenerate = vi.fn()
    renderCard({ view: macroView({ plan: { stale_reason: 'races_changed' } as MacroPlanView['plan'] }), onRegenerate })

    const banner = within(screen.getByRole('status'))
    expect(banner.getByText('Your races changed since this plan was made')).toBeInTheDocument()

    fireEvent.click(banner.getByRole('button', { name: 'Regenerate' }))
    // The banner goes through the same confirmation as the toolbar button.
    expect(onRegenerate).not.toHaveBeenCalled()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Regenerate' }))
    expect(onRegenerate).toHaveBeenCalledTimes(1)
  })

  it('falls back to a generic message for an unknown stale reason', () => {
    renderCard({ view: macroView({ plan: { stale_reason: 'something_new' } as MacroPlanView['plan'] }) })

    expect(within(screen.getByRole('status')).getByText('This plan is out of date')).toBeInTheDocument()
  })
})

describe('LongTermPlanCard – regenerate', () => {
  it('asks for confirmation before regenerating', () => {
    const onRegenerate = vi.fn()
    renderCard({ onRegenerate })

    fireEvent.click(screen.getByRole('button', { name: 'Regenerate' }))

    const dialog = within(screen.getByRole('dialog'))
    expect(dialog.getByText('Regenerate the long-term plan?')).toBeInTheDocument()
    expect(
      dialog.getByText('This replaces the plan from next Monday; weeks already planned are kept.'),
    ).toBeInTheDocument()
    expect(onRegenerate).not.toHaveBeenCalled()
  })

  it('does not regenerate when the dialog is cancelled', () => {
    const onRegenerate = vi.fn()
    renderCard({ onRegenerate })

    fireEvent.click(screen.getByRole('button', { name: 'Regenerate' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))

    expect(onRegenerate).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('regenerates on confirm and closes the dialog', () => {
    const onRegenerate = vi.fn()
    renderCard({ onRegenerate })

    fireEvent.click(screen.getByRole('button', { name: 'Regenerate' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Regenerate' }))

    expect(onRegenerate).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('disables both actions while a regeneration is running', () => {
    renderCard({ busyAction: 'regenerate' })

    expect(screen.getByRole('button', { name: 'Regenerating…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Extend' })).toBeDisabled()
  })

  it('shows a failed action without dropping the block from the page', () => {
    renderCard({ actionError: 'a generation is already running — try again in a moment' })

    expect(screen.getByRole('alert')).toHaveTextContent('a generation is already running')
    expect(screen.getByText('Run 1:25 for the half marathon')).toBeInTheDocument()
  })
})

describe('LongTermPlanCard – extend', () => {
  it('is enabled when the horizon is inside the lead window and nothing is queued', () => {
    const onExtend = vi.fn()
    renderCard({ onExtend })

    const extend = screen.getByRole('button', { name: 'Extend' })
    expect(extend).toBeEnabled()
    fireEvent.click(extend)
    expect(onExtend).toHaveBeenCalledTimes(1)
  })

  it('is disabled while more than the lead window remains', () => {
    const view = macroView()
    view.plan.end_week = mondayOffset(EXTENSION_LEAD_WEEKS + 1)
    renderCard({ view })

    const extend = screen.getByRole('button', { name: 'Extend' })
    expect(extend).toBeDisabled()
    expect(extend).toHaveAttribute(
      'title',
      'Available once 8 weeks or less remain in this block.',
    )
  })

  it('is enabled on the boundary week of the lead window', () => {
    const view = macroView()
    view.plan.end_week = mondayOffset(EXTENSION_LEAD_WEEKS)
    renderCard({ view })

    expect(screen.getByRole('button', { name: 'Extend' })).toBeEnabled()
  })

  it('is disabled when a block is already queued behind this one', () => {
    const onExtend = vi.fn()
    renderCard({ view: macroView({ has_next_block: true }), onExtend })

    const extend = screen.getByRole('button', { name: 'Extend' })
    expect(extend).toBeDisabled()
    expect(extend).toHaveAttribute('title', 'A new block is already queued after this one.')
    fireEvent.click(extend)
    expect(onExtend).not.toHaveBeenCalled()
  })

  it('spins its own button while extending', () => {
    renderCard({ busyAction: 'extend' })

    expect(screen.getByRole('button', { name: 'Extending…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Regenerate' })).toBeDisabled()
  })
})
