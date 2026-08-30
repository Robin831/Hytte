// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import TrainingSection from './TrainingSection'

// i18n: return the key verbatim so the assertions target keys, not copy.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', changeLanguage: () => {} },
  }),
}))

function renderSection(
  preferences: Record<string, string>,
  overrides: Partial<{
    hasStride: boolean
    queuePreference: (key: string, value: string) => void
    savePreference: (key: string, value: string) => Promise<void>
  }> = {},
) {
  const queuePreference = overrides.queuePreference ?? vi.fn<(key: string, value: string) => void>()
  const props = {
    preferences,
    saving: false,
    savePreference: overrides.savePreference ?? vi.fn(async () => {}),
    savePreferences: vi.fn(async () => {}),
    queuePreference,
    flushPreferences: vi.fn(),
    hasStride: overrides.hasStride ?? true,
  }
  const view = render(<TrainingSection {...props} />)
  return { ...view, queuePreference, props }
}

const days = () => screen.getByLabelText('training.strideAvailableDays') as HTMLInputElement
const cap = () => screen.getByLabelText('training.strideWeeklyDistanceCap') as HTMLInputElement

describe('TrainingSection – Stride feature gate', () => {
  it('hides every Stride-only control without the feature', () => {
    renderSection({ stride_enabled: 'true' }, { hasStride: false })

    expect(screen.queryByText('training.strideHeading')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('training.enableStride')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('training.strideAvailableDays')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('training.strideWeeklyDistanceCap')).not.toBeInTheDocument()
    expect(screen.queryByText('training.strideCustomPrompt')).not.toBeInTheDocument()
    expect(screen.queryByText('training.strideTreadmillCalibration')).not.toBeInTheDocument()
  })

  it('shows them with the feature', () => {
    renderSection({}, { hasStride: true })

    expect(screen.getByText('training.strideHeading')).toBeInTheDocument()
    expect(screen.getByLabelText('training.enableStride')).toBeInTheDocument()
    expect(screen.getByText('training.strideCustomPrompt')).toBeInTheDocument()
    expect(screen.getByText('training.strideTreadmillCalibration')).toBeInTheDocument()
  })

  it('uses a dedicated label for each toggle state', () => {
    const savePreference = vi.fn(async () => {})
    const { rerender, props } = renderSection({ stride_enabled: 'false' }, { savePreference })

    fireEvent.click(screen.getByLabelText('training.enableStride'))
    expect(savePreference).toHaveBeenCalledWith('stride_enabled', 'true')

    rerender(<TrainingSection {...props} preferences={{ stride_enabled: 'true' }} />)
    expect(screen.getByLabelText('training.disableStride')).toBeInTheDocument()
  })
})

describe('TrainingSection – Stride numeric preferences', () => {
  let queuePreference: ReturnType<typeof vi.fn<(key: string, value: string) => void>>

  beforeEach(() => {
    queuePreference = vi.fn<(key: string, value: string) => void>()
  })

  it('shows preferences that arrive after mount', () => {
    const { rerender, props } = renderSection({}, { queuePreference })
    expect(days().value).toBe('')

    // The preference map is fetched asynchronously; a late arrival must reach
    // the inputs rather than leaving them blank for values that are set.
    rerender(
      <TrainingSection
        {...props}
        preferences={{ stride_available_days: '5', stride_weekly_distance_cap: '70' }}
      />,
    )
    expect(days().value).toBe('5')
    expect(cap().value).toBe('70')
  })

  it('queues in-range values on blur', () => {
    renderSection({}, { queuePreference })

    fireEvent.change(days(), { target: { value: '4' } })
    fireEvent.blur(days())
    expect(queuePreference).toHaveBeenCalledWith('stride_available_days', '4')

    fireEvent.change(cap(), { target: { value: '80' } })
    fireEvent.blur(cap())
    expect(queuePreference).toHaveBeenCalledWith('stride_weekly_distance_cap', '80')
  })

  it('clears the preference when the field is emptied', () => {
    renderSection({ stride_available_days: '5' }, { queuePreference })

    fireEvent.change(days(), { target: { value: '' } })
    fireEvent.blur(days())
    expect(queuePreference).toHaveBeenCalledWith('stride_available_days', '')
  })

  it('normalises leading zeros', () => {
    renderSection({}, { queuePreference })

    fireEvent.change(days(), { target: { value: '07' } })
    fireEvent.blur(days())
    expect(queuePreference).toHaveBeenCalledWith('stride_available_days', '7')
  })

  // Out-of-range values never reach the server, and the field goes back to what
  // is saved rather than sitting on a value that was silently not written.
  // (Text like "3abc" is sanitised to "" by the number input before it reaches
  // the handler; the digits-only guard covers keyboards that do not sanitise.)
  it('rejects out-of-range drafts and reverts to the stored value', () => {
    renderSection({ stride_available_days: '5', stride_weekly_distance_cap: '70' }, { queuePreference })

    for (const bad of ['0', '8']) {
      fireEvent.change(days(), { target: { value: bad } })
      fireEvent.blur(days())
      expect(queuePreference).not.toHaveBeenCalled()
      expect(days().value).toBe('5')
    }

    for (const bad of ['0', '501']) {
      fireEvent.change(cap(), { target: { value: bad } })
      fireEvent.blur(cap())
      expect(queuePreference).not.toHaveBeenCalled()
      expect(cap().value).toBe('70')
    }
  })
})
