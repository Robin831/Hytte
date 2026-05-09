// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import WorkoutContextModal from './WorkoutContextModal'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', changeLanguage: () => {} },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('./SpeedPlanEditor', () => ({
  __esModule: true,
  default: () => <div data-testid="speed-plan-editor-mock">speed plan editor</div>,
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

  it('renders SpeedPlanEditor only when surface is Treadmill', () => {
    render(
      <WorkoutContextModal workoutId="42" isOpen={true} onClose={() => {}} />,
    )

    // Initially no surface selected → no speed plan editor.
    expect(screen.queryByTestId('speed-plan-editor-mock')).toBeNull()

    // Select Outside → still no editor.
    fireEvent.click(screen.getByTestId('toggle-surface-Outside'))
    expect(screen.queryByTestId('speed-plan-editor-mock')).toBeNull()

    // Select Treadmill → editor renders.
    fireEvent.click(screen.getByTestId('toggle-surface-Treadmill'))
    expect(screen.getByTestId('speed-plan-editor-mock')).toBeTruthy()
  })

  it('renders SpeedPlanEditor when initialContext.surface is Treadmill', () => {
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
    expect(screen.getByTestId('speed-plan-editor-mock')).toBeTruthy()
    expect(screen.getByTestId('toggle-surface-Treadmill')).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('toggle-runType-interval')).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('toggle-hrSource-chest')).toHaveAttribute('aria-checked', 'true')
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
