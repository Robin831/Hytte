// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { TrainingBlockTimeline } from './TrainingBlockTimeline'
import enStride from '../../../public/locales/en/stride.json'
import type { MacroPlanView, MacroWeek } from '../../types/stride'

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
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

// Monday of the week `offset` weeks from the current one, as YYYY-MM-DD, so the
// block always straddles "today" regardless of when the suite runs.
function mondayOffset(offset: number): string {
  const d = new Date()
  d.setHours(0, 0, 0, 0)
  d.setDate(d.getDate() - ((d.getDay() + 6) % 7) + offset * 7)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function isoDate(daysFromToday: number): string {
  const d = new Date()
  d.setDate(d.getDate() + daysFromToday)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const A_RACE = {
  id: 1,
  name: 'Oslo Half',
  date: isoDate(90),
  priority: 'A' as const,
  result_time: null,
}

// A 12-week block starting two weeks ago: 4 base, 4 build, 2 recovery, 2 peak.
// Recovery never appears in the A-race fallback, so asserting on it proves the
// phases came from the macro plan.
const PHASE_PLAN: Array<[string, string]> = [
  ['base', 'Aerobic base'],
  ['base', 'Aerobic base'],
  ['base', 'Aerobic base'],
  ['base', 'Aerobic base'],
  ['build', 'Threshold build'],
  ['build', 'Threshold build'],
  ['build', 'Threshold build'],
  ['build', 'Threshold build'],
  ['recovery', 'Down weeks'],
  ['recovery', 'Down weeks'],
  ['peak', 'Race sharpening'],
  ['peak', 'Race sharpening'],
]

const MACRO_WEEKS: MacroWeek[] = PHASE_PLAN.map(([phase, mesocycle], i) => ({
  id: i + 1,
  macro_plan_id: 7,
  user_id: 1,
  week_start: mondayOffset(i - 2),
  seq: i + 1,
  phase,
  mesocycle,
  load_level: 'normal',
  target_km: 50,
  target_sessions: 5,
  race_id: null,
  key_sessions: [],
  intent: '',
  status: 'planned',
}))

function macroView(overrides: Partial<MacroPlanView> = {}): MacroPlanView {
  return {
    plan: {
      id: 7,
      user_id: 1,
      start_week: MACRO_WEEKS[0].week_start,
      end_week: MACRO_WEEKS[MACRO_WEEKS.length - 1].week_start,
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
    weeks: MACRO_WEEKS,
    current_goal_revision: null,
    revisions: [],
    ...overrides,
  }
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('TrainingBlockTimeline – no macro plan (A-race fallback)', () => {
  it('derives the phases from the next A-priority race', () => {
    render(<TrainingBlockTimeline races={[A_RACE]} />)

    expect(screen.getByRole('img', { name: enStride.timeline.ariaLabel })).toBeInTheDocument()
    expect(screen.getByText('Oslo Half')).toBeInTheDocument()
    // The heuristic back-fills taper 2 / peak 4 / build 6 from race day.
    expect(screen.getAllByText('Taper').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Peak').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Build').length).toBeGreaterThan(0)
    expect(screen.queryByText('Recovery')).not.toBeInTheDocument()
    expect(screen.queryByTestId('timeline-current-week')).not.toBeInTheDocument()
  })

  it('prompts for an A-priority race when there is neither a macro plan nor a goal race', () => {
    render(<TrainingBlockTimeline races={[]} macroPlan={null} />)
    expect(screen.getByText(enStride.timeline.noGoalRace)).toBeInTheDocument()
  })

  it('renders nothing while races are still loading', () => {
    const { container } = render(<TrainingBlockTimeline races={[A_RACE]} loading />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('TrainingBlockTimeline – macro plan present', () => {
  it('renders the macro plan mesocycles, per-week phases and the current week', () => {
    render(<TrainingBlockTimeline races={[]} macroPlan={macroView()} />)

    expect(screen.getByRole('img', { name: enStride.timeline.macroAriaLabel })).toBeInTheDocument()

    // Mesocycles come from the block's weeks, collapsed into runs.
    expect(screen.getByText('Aerobic base')).toBeInTheDocument()
    expect(screen.getByText('Threshold build')).toBeInTheDocument()
    expect(screen.getByText('Down weeks')).toBeInTheDocument()
    expect(screen.getByText('Race sharpening')).toBeInTheDocument()

    // Phase segments, including one the A-race heuristic cannot produce.
    expect(screen.getByText('Recovery')).toBeInTheDocument()

    // Week 3 of the block is in progress, so it is outlined and the phase chip
    // shows that week's phase.
    expect(screen.getByTestId('timeline-current-week')).toBeInTheDocument()
    expect(screen.getAllByText('Base').length).toBeGreaterThan(0)

    // No A-race, so the countdown is the block's remaining weeks (12 - 2).
    expect(screen.getByText('10 weeks left in block')).toBeInTheDocument()
    expect(screen.getByText('Run 1:25 for the half marathon')).toBeInTheDocument()
  })

  it('keeps the macro phases when an A-priority race also exists', () => {
    render(<TrainingBlockTimeline races={[A_RACE]} macroPlan={macroView()} />)

    expect(screen.getByRole('img', { name: enStride.timeline.macroAriaLabel })).toBeInTheDocument()
    expect(screen.getByText('Recovery')).toBeInTheDocument()
    // The race is still the thing being counted down to.
    expect(screen.getByText('Oslo Half')).toBeInTheDocument()
    expect(screen.getByText(/weeks to goal/)).toBeInTheDocument()
  })

  it('falls back to the A-race heuristic when the block has no week rows', () => {
    render(<TrainingBlockTimeline races={[A_RACE]} macroPlan={macroView({ weeks: [] })} />)
    expect(screen.getByRole('img', { name: enStride.timeline.ariaLabel })).toBeInTheDocument()
  })
})
