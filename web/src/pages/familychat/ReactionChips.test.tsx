// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import ReactionChips from './ReactionChips'
import type { ReactionMap } from './api'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, string | number>) => {
      if (opts) return `${key}:${JSON.stringify(opts)}`
      return key
    },
  }),
}))

describe('ReactionChips', () => {
  it('renders nothing when reactions are undefined', () => {
    const { container } = render(<ReactionChips reactions={undefined} onToggle={vi.fn()} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders nothing for an empty reaction map', () => {
    const { container } = render(<ReactionChips reactions={{}} onToggle={vi.fn()} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders a chip per emoji with its count', () => {
    const reactions: ReactionMap = {
      '👍': { count: 2, users: [1, 2], me: false },
      '🎉': { count: 1, users: [3], me: false },
    }
    render(<ReactionChips reactions={reactions} onToggle={vi.fn()} />)
    expect(screen.getByTestId('reaction-chip-👍').textContent).toContain('2')
    expect(screen.getByTestId('reaction-chip-🎉').textContent).toContain('1')
  })

  it('marks own reactions with aria-pressed=true', () => {
    const reactions: ReactionMap = {
      '👍': { count: 1, users: [1], me: true },
      '😢': { count: 1, users: [2], me: false },
    }
    render(<ReactionChips reactions={reactions} onToggle={vi.fn()} />)
    expect(screen.getByTestId('reaction-chip-👍').getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByTestId('reaction-chip-😢').getAttribute('aria-pressed')).toBe('false')
  })

  it('invokes onToggle with the chip emoji and current me-flag', () => {
    const onToggle = vi.fn()
    const reactions: ReactionMap = { '👍': { count: 1, users: [1], me: true } }
    render(<ReactionChips reactions={reactions} onToggle={onToggle} />)
    fireEvent.click(screen.getByTestId('reaction-chip-👍'))
    expect(onToggle).toHaveBeenCalledWith('👍', true)
  })

  it('skips buckets whose count has dropped to zero', () => {
    const reactions: ReactionMap = {
      '👍': { count: 0, users: [], me: false },
      '🎉': { count: 1, users: [1], me: false },
    }
    render(<ReactionChips reactions={reactions} onToggle={vi.fn()} />)
    expect(screen.queryByTestId('reaction-chip-👍')).not.toBeInTheDocument()
    expect(screen.getByTestId('reaction-chip-🎉')).toBeInTheDocument()
  })
})
