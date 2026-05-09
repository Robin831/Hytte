// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import WorkoutContextModal from './WorkoutContextModal'
import type { SpeedPlanSegment } from '../../types/training'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', changeLanguage: () => {} },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

describe('WorkoutContextModal', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ context: {} }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders nothing when isOpen is false', () => {
    render(
      <WorkoutContextModal workoutId="42" isOpen={false} onClose={() => {}} />,
    )
    expect(screen.queryByText('workoutContextModal.title')).toBeNull()
  })

  it('updates the active option when each toggle is clicked', () => {
    render(
      <WorkoutContextModal workoutId="42" isOpen={true} onClose={() => {}} />,
    )

    const surfaceOutside = screen.getByTestId('toggle-surface-Outside')
    fireEvent.click(surfaceOutside)
    expect(surfaceOutside).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('toggle-surface-Treadmill')).toHaveAttribute('aria-checked', 'false')

    const runTypeInterval = screen.getByTestId('toggle-runType-interval')
    fireEvent.click(runTypeInterval)
    expect(runTypeInterval).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('toggle-runType-slow')).toHaveAttribute('aria-checked', 'false')

    const hrSourceWatch = screen.getByTestId('toggle-hrSource-watch')
    fireEvent.click(hrSourceWatch)
    expect(hrSourceWatch).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('toggle-hrSource-chest')).toHaveAttribute('aria-checked', 'false')
  })

  it('renders speed plan section only when surface is Treadmill', () => {
    render(
      <WorkoutContextModal workoutId="42" isOpen={true} onClose={() => {}} />,
    )

    expect(screen.queryByTestId('speed-plan-editor')).toBeNull()

    fireEvent.click(screen.getByTestId('toggle-surface-Outside'))
    expect(screen.queryByTestId('speed-plan-editor')).toBeNull()

    fireEvent.click(screen.getByTestId('toggle-surface-Treadmill'))
    expect(screen.getByTestId('speed-plan-editor')).toBeTruthy()
  })

  it('renders speed plan section when initialContext.surface is Treadmill', () => {
    render(
      <WorkoutContextModal
        workoutId="42"
        isOpen={true}
        onClose={() => {}}
        initialContext={{
          surface: 'Treadmill',
          run_type: 'interval',
          hr_source: 'chest',
          feel_notes: '',
          speed_plan: [],
        }}
      />,
    )
    expect(screen.getByTestId('speed-plan-editor')).toBeTruthy()
    expect(screen.getByTestId('toggle-surface-Treadmill')).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('toggle-runType-interval')).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('toggle-hrSource-chest')).toHaveAttribute('aria-checked', 'true')
  })

  it('normalizes lowercase surface from backend — "treadmill" shows as selected and renders speed plan section', () => {
    render(
      <WorkoutContextModal
        workoutId="42"
        isOpen={true}
        onClose={() => {}}
        initialContext={{
          surface: 'treadmill',
          run_type: '',
          hr_source: '',
          feel_notes: '',
          speed_plan: [],
        }}
      />,
    )
    expect(screen.getByTestId('speed-plan-editor')).toBeTruthy()
    expect(screen.getByTestId('toggle-surface-Treadmill')).toHaveAttribute('aria-checked', 'true')
  })

  it('clears the speed plan when surface toggles away from Treadmill', async () => {
    const onClose = vi.fn()
    render(
      <WorkoutContextModal
        workoutId="42"
        isOpen={true}
        onClose={onClose}
        initialContext={{
          surface: 'Treadmill',
          run_type: 'slow',
          hr_source: 'chest',
          feel_notes: '',
          speed_plan: [
            { kind: 'warmup', speed_kmph: 8, duration_sec: 600, repeats: 1, same_as_previous: false },
          ],
        }}
      />,
    )

    fireEvent.click(screen.getByTestId('toggle-surface-Outside'))
    expect(screen.queryByTestId('speed-plan-editor')).toBeNull()

    fireEvent.click(screen.getByText('workoutContextModal.save'))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    const body = JSON.parse(fetchMock.mock.calls[0][1]?.body as string)
    expect(body.speed_plan).toEqual([])
  })

  it('PUTs the form state to the workout context endpoint and calls onClose on success', async () => {
    const onClose = vi.fn()
    render(
      <WorkoutContextModal workoutId="42" isOpen={true} onClose={onClose} />,
    )

    fireEvent.click(screen.getByTestId('toggle-surface-Outside'))
    fireEvent.click(screen.getByTestId('toggle-runType-slow'))
    fireEvent.click(screen.getByTestId('toggle-hrSource-watch'))

    const textarea = screen.getByPlaceholderText('workoutContextModal.feelNotes.placeholder')
    fireEvent.change(textarea, { target: { value: 'felt strong' } })

    fireEvent.click(screen.getByText('workoutContextModal.save'))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/training/workouts/42/context')
    expect(init?.method).toBe('PUT')
    expect(init?.credentials).toBe('include')

    const body = JSON.parse(init?.body as string)
    expect(body).toEqual({
      surface: 'Outside',
      run_type: 'slow',
      hr_source: 'watch',
      feel_notes: 'felt strong',
      speed_plan: [],
    })

    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })

  describe('SpeedPlanEditor integration', () => {
    function openTreadmillModal(initialSegments: SpeedPlanSegment[] = []) {
      return render(
        <WorkoutContextModal
          workoutId="42"
          isOpen={true}
          onClose={() => {}}
          initialContext={{
            surface: 'Treadmill',
            run_type: 'interval',
            hr_source: 'chest',
            feel_notes: '',
            speed_plan: initialSegments,
          }}
        />,
      )
    }

    async function saveAndReadBody(): Promise<Record<string, unknown>> {
      fireEvent.click(screen.getByText('workoutContextModal.save'))
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
      return JSON.parse(fetchMock.mock.calls[0][1]?.body as string)
    }

    it('appends a new segment when the + button is clicked', async () => {
      openTreadmillModal()

      // Default add-kind is 'interval'.
      fireEvent.click(screen.getByTestId('speed-plan-add'))
      expect(screen.getByTestId('speed-plan-row-0')).toBeTruthy()

      const body = await saveAndReadBody()
      expect(body.speed_plan).toEqual([
        { kind: 'interval', speed_kmph: 0, duration_sec: 0, repeats: 1, same_as_previous: false },
      ])
    })

    it('removes a segment when the row remove button is clicked', async () => {
      openTreadmillModal([
        { kind: 'warmup', speed_kmph: 8, duration_sec: 600, repeats: 1, same_as_previous: false },
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
      ])

      fireEvent.click(screen.getByTestId('speed-plan-row-0-remove'))

      const body = await saveAndReadBody()
      expect(body.speed_plan).toEqual([
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
      ])
    })

    it('with same-speed ON, the shared interval speed input updates every interval segment', async () => {
      openTreadmillModal([
        { kind: 'warmup', speed_kmph: 8, duration_sec: 600, repeats: 1, same_as_previous: false },
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
      ])

      // Toggle is ON by default — shared input is visible.
      const sharedSpeed = screen.getByTestId('speed-plan-shared-interval-speed') as HTMLInputElement
      expect(sharedSpeed.value).toBe('14')

      fireEvent.change(sharedSpeed, { target: { value: '16.5' } })

      const body = await saveAndReadBody()
      const segs = body.speed_plan as Array<{ kind: string; speed_kmph: number }>
      const intervalSpeeds = segs.filter(s => s.kind === 'interval').map(s => s.speed_kmph)
      expect(intervalSpeeds).toEqual([16.5, 16.5, 16.5])
      // Warm-up untouched.
      expect(segs.find(s => s.kind === 'warmup')?.speed_kmph).toBe(8)
    })

    it('with same-speed ON, per-row speed inputs are hidden for interval rows but shown for non-interval rows', () => {
      openTreadmillModal([
        { kind: 'warmup', speed_kmph: 8, duration_sec: 600, repeats: 1, same_as_previous: false },
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
      ])

      // Warmup row (index 0) keeps its per-row speed input.
      expect(screen.getByTestId('speed-plan-row-0-speed')).toBeTruthy()
      // Interval row (index 1) does not show a per-row speed input while collapsed.
      expect(screen.queryByTestId('speed-plan-row-1-speed')).toBeNull()
      // But the shared interval speed input is visible.
      expect(screen.getByTestId('speed-plan-shared-interval-speed')).toBeTruthy()
    })

    it('toggling same-speed OFF reveals per-row interval speed inputs and preserves existing values', async () => {
      openTreadmillModal([
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
      ])

      const toggle = screen.getByTestId('speed-plan-same-speed-toggle') as HTMLInputElement
      expect(toggle.checked).toBe(true)

      fireEvent.click(toggle)
      expect(toggle.checked).toBe(false)

      // Shared input is gone, per-row speed inputs are shown.
      expect(screen.queryByTestId('speed-plan-shared-interval-speed')).toBeNull()
      const row0Speed = screen.getByTestId('speed-plan-row-0-speed') as HTMLInputElement
      const row1Speed = screen.getByTestId('speed-plan-row-1-speed') as HTMLInputElement
      expect(row0Speed.value).toBe('14')
      expect(row1Speed.value).toBe('14')

      // Edit only row 1 — the values diverge.
      fireEvent.change(row1Speed, { target: { value: '17' } })

      const body = await saveAndReadBody()
      const speeds = (body.speed_plan as Array<{ speed_kmph: number }>).map(s => s.speed_kmph)
      expect(speeds).toEqual([14, 17])
    })

    it('shared pause speed and duration update every pause segment', async () => {
      openTreadmillModal([
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
        { kind: 'pause', speed_kmph: 6, duration_sec: 30, repeats: 1, same_as_previous: false },
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
        { kind: 'pause', speed_kmph: 6, duration_sec: 30, repeats: 1, same_as_previous: false },
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
      ])

      const sharedPauseSpeed = screen.getByTestId('speed-plan-shared-pause-speed') as HTMLInputElement
      const sharedPauseDuration = screen.getByTestId('speed-plan-shared-pause-duration') as HTMLInputElement
      expect(sharedPauseSpeed.value).toBe('6')
      expect(sharedPauseDuration.value).toBe('30')

      fireEvent.change(sharedPauseSpeed, { target: { value: '5.5' } })
      fireEvent.change(sharedPauseDuration, { target: { value: '45' } })

      const body = await saveAndReadBody()
      const pauses = (body.speed_plan as Array<{ kind: string; speed_kmph: number; duration_sec: number }>)
        .filter(s => s.kind === 'pause')
      expect(pauses).toEqual([
        { kind: 'pause', speed_kmph: 5.5, duration_sec: 45, repeats: 1, same_as_previous: false },
        { kind: 'pause', speed_kmph: 5.5, duration_sec: 45, repeats: 1, same_as_previous: false },
      ])
    })

    it('appending a pause inherits the shared pause speed and duration', async () => {
      openTreadmillModal([
        { kind: 'pause', speed_kmph: 5.5, duration_sec: 45, repeats: 1, same_as_previous: false },
      ])

      const select = screen.getByTestId('speed-plan-add-kind') as HTMLSelectElement
      fireEvent.change(select, { target: { value: 'pause' } })
      fireEvent.click(screen.getByTestId('speed-plan-add'))

      const body = await saveAndReadBody()
      const pauses = (body.speed_plan as Array<{ kind: string; speed_kmph: number; duration_sec: number }>)
        .filter(s => s.kind === 'pause')
      expect(pauses).toEqual([
        { kind: 'pause', speed_kmph: 5.5, duration_sec: 45, repeats: 1, same_as_previous: false },
        { kind: 'pause', speed_kmph: 5.5, duration_sec: 45, repeats: 1, same_as_previous: false },
      ])
    })

    it('initializes same-speed toggle to false when interval speeds differ', () => {
      openTreadmillModal([
        { kind: 'interval', speed_kmph: 14, duration_sec: 60, repeats: 1, same_as_previous: false },
        { kind: 'interval', speed_kmph: 16, duration_sec: 60, repeats: 1, same_as_previous: false },
      ])

      const toggle = screen.getByTestId('speed-plan-same-speed-toggle') as HTMLInputElement
      expect(toggle.checked).toBe(false)
      // Per-row speed inputs should be visible for each interval
      expect(screen.getByTestId('speed-plan-row-0-speed')).toBeTruthy()
      expect(screen.getByTestId('speed-plan-row-1-speed')).toBeTruthy()
      // Shared speed input should not be rendered while toggle is off
      expect(screen.queryByTestId('speed-plan-shared-interval-speed')).toBeNull()
    })

    it('normalizes inconsistent pause segments to first pause values on mount', async () => {
      openTreadmillModal([
        { kind: 'pause', speed_kmph: 5, duration_sec: 30, repeats: 1, same_as_previous: false },
        { kind: 'pause', speed_kmph: 7, duration_sec: 45, repeats: 1, same_as_previous: false },
      ])

      // Normalization is synchronous (in initForm) and 'speed_plan' is pre-added to
      // touched, so saving immediately sends the corrected segments.
      const body = await saveAndReadBody()
      const pauses = (body.speed_plan as Array<{ kind: string; speed_kmph: number; duration_sec: number }>)
        .filter(s => s.kind === 'pause')
      expect(pauses).toEqual([
        { kind: 'pause', speed_kmph: 5, duration_sec: 30, repeats: 1, same_as_previous: false },
        { kind: 'pause', speed_kmph: 5, duration_sec: 30, repeats: 1, same_as_previous: false },
      ])
    })

    it('supports adding all four kinds via the kind selector', async () => {
      openTreadmillModal()

      const select = screen.getByTestId('speed-plan-add-kind') as HTMLSelectElement
      const add = screen.getByTestId('speed-plan-add')

      for (const kind of ['warmup', 'interval', 'pause', 'cooldown'] as const) {
        fireEvent.change(select, { target: { value: kind } })
        fireEvent.click(add)
      }

      const body = await saveAndReadBody()
      const kinds = (body.speed_plan as Array<{ kind: string }>).map(s => s.kind)
      expect(kinds).toEqual(['warmup', 'interval', 'pause', 'cooldown'])
    })
  })

  it('shows an error and does not close when the save request fails', async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'boom' }), {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    const onClose = vi.fn()
    render(
      <WorkoutContextModal workoutId="42" isOpen={true} onClose={onClose} />,
    )

    fireEvent.click(screen.getByText('workoutContextModal.save'))

    await waitFor(() => expect(screen.getByText('boom')).toBeTruthy())
    expect(onClose).not.toHaveBeenCalled()
  })
})
