// @vitest-environment happy-dom
import { useState } from 'react'
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import SpeedPlanEditor from './SpeedPlanEditor'
import type { SpeedPlanSegment } from '../../types/training'

function StatefulEditorWithSpy({ initial, spy }: { initial: SpeedPlanSegment[], spy: (s: SpeedPlanSegment[]) => void }) {
  const [segments, setSegments] = useState<SpeedPlanSegment[]>(initial)
  function handleChange(segs: SpeedPlanSegment[]) {
    setSegments(segs)
    spy(segs)
  }
  return <SpeedPlanEditor value={segments} onChange={handleChange} />
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', changeLanguage: () => {} },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

function intervalSeg(durationSec: number): SpeedPlanSegment {
  return { kind: 'interval', speed_kmph: 12, duration_sec: durationSec, repeats: 1, same_as_previous: false }
}

function pauseSeg(durationSec: number): SpeedPlanSegment {
  return { kind: 'pause', speed_kmph: 6, duration_sec: durationSec, repeats: 1, same_as_previous: false }
}

describe('SpeedPlanEditor duration inputs', () => {
  it('splits duration_sec=90 into 1m 30s on the per-row interval', () => {
    render(<SpeedPlanEditor value={[intervalSeg(90)]} onChange={() => {}} />)

    const minutes = screen.getByTestId('speed-plan-row-0-duration-minutes') as HTMLInputElement
    const seconds = screen.getByTestId('speed-plan-row-0-duration-seconds') as HTMLInputElement
    expect(minutes.value).toBe('1')
    expect(seconds.value).toBe('30')
  })

  it('splits a legacy duration_sec=75 into 1m 15s (backwards compatible)', () => {
    render(<SpeedPlanEditor value={[intervalSeg(75)]} onChange={() => {}} />)

    const minutes = screen.getByTestId('speed-plan-row-0-duration-minutes') as HTMLInputElement
    const seconds = screen.getByTestId('speed-plan-row-0-duration-seconds') as HTMLInputElement
    expect(minutes.value).toBe('1')
    expect(seconds.value).toBe('15')
  })

  it('combines minutes=2 + seconds=30 into duration_sec=150 on the per-row interval', () => {
    const spy = vi.fn()
    render(<StatefulEditorWithSpy initial={[intervalSeg(0)]} spy={spy} />)

    fireEvent.change(screen.getByTestId('speed-plan-row-0-duration-minutes'), { target: { value: '2' } })
    fireEvent.change(screen.getByTestId('speed-plan-row-0-duration-seconds'), { target: { value: '30' } })

    const lastCall = spy.mock.calls[spy.mock.calls.length - 1][0] as SpeedPlanSegment[]
    expect(lastCall[0].duration_sec).toBe(150)
  })

  it('emits duration_sec=120 when minutes is set to 2 from a zero start', () => {
    const onChange = vi.fn()
    render(<SpeedPlanEditor value={[intervalSeg(0)]} onChange={onChange} />)

    fireEvent.change(screen.getByTestId('speed-plan-row-0-duration-minutes'), { target: { value: '2' } })
    expect(onChange).toHaveBeenLastCalledWith([
      expect.objectContaining({ kind: 'interval', duration_sec: 120 }),
    ])
  })

  it('renders the shared pause duration as min+sec inputs', () => {
    render(<SpeedPlanEditor value={[pauseSeg(75)]} onChange={() => {}} />)

    const minutes = screen.getByTestId('speed-plan-shared-pause-duration-minutes') as HTMLInputElement
    const seconds = screen.getByTestId('speed-plan-shared-pause-duration-seconds') as HTMLInputElement
    expect(minutes.value).toBe('1')
    expect(seconds.value).toBe('15')
  })

  it('updates duration_sec across all pause segments when shared pause min/sec changes', () => {
    const onChange = vi.fn()
    render(<SpeedPlanEditor value={[pauseSeg(0), pauseSeg(0)]} onChange={onChange} />)

    fireEvent.change(screen.getByTestId('speed-plan-shared-pause-duration-minutes'), { target: { value: '2' } })
    const calls = onChange.mock.calls
    const last = calls[calls.length - 1][0] as SpeedPlanSegment[]
    expect(last).toHaveLength(2)
    expect(last.every((s) => s.kind === 'pause' && s.duration_sec === 120)).toBe(true)
  })

  it('clamps seconds input above 59 down to 59', () => {
    const onChange = vi.fn()
    render(<SpeedPlanEditor value={[intervalSeg(60)]} onChange={onChange} />)

    fireEvent.change(screen.getByTestId('speed-plan-row-0-duration-seconds'), { target: { value: '90' } })
    expect(onChange).toHaveBeenLastCalledWith([
      expect.objectContaining({ duration_sec: 60 + 59 }),
    ])
  })

  it('treats an empty seconds value as 0', () => {
    const onChange = vi.fn()
    render(<SpeedPlanEditor value={[intervalSeg(150)]} onChange={onChange} />)

    fireEvent.change(screen.getByTestId('speed-plan-row-0-duration-seconds'), { target: { value: '' } })
    expect(onChange).toHaveBeenLastCalledWith([
      expect.objectContaining({ duration_sec: 120 }),
    ])
  })
})
