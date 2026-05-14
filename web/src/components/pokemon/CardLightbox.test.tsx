// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import CardLightbox, { type LightboxCard } from './CardLightbox'

const TRANSLATIONS: Record<string, string> = {
  'lightbox.previousCard': 'Previous card',
  'lightbox.nextCard': 'Next card',
  'lightbox.close': 'Close lightbox',
  'lightbox.wrappedToStart': 'Wrapped to the first card',
  'lightbox.wrappedToEnd': 'Wrapped to the last card',
  'detail.ownership': 'Owned',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  if (key === 'tile.collectorNo') return `#${opts?.number ?? ''}`
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../../i18n', () => ({
  default: { language: 'en' },
}))

function makeCard(over: Partial<LightboxCard> = {}): LightboxCard {
  return {
    id: 'c0',
    name: 'Card 0',
    collector_no: '001',
    set_id: 'sv1',
    set_name: 'Test Set',
    image_large_url: 'https://example.com/0.png',
    variants: [{ price_eur: 1, price_nok: 10, owned: false }],
    ...over,
  }
}

const fixture: LightboxCard[] = [
  makeCard({ id: 'c0', name: 'Card 0', image_large_url: 'https://example.com/0.png' }),
  makeCard({ id: 'c1', name: 'Card 1', image_large_url: 'https://example.com/1.png' }),
  makeCard({ id: 'c2', name: 'Card 2', image_large_url: 'https://example.com/2.png' }),
]

afterEach(() => {
  vi.clearAllMocks()
})

function getImage(): HTMLImageElement {
  return screen.getByTestId('lightbox-image') as HTMLImageElement
}

describe('CardLightbox – initial render', () => {
  it('renders the dialog at the start index with the correct image', () => {
    const onClose = vi.fn()
    render(<CardLightbox cards={fixture} startIndex={1} onClose={onClose} />)
    const dialog = screen.getByRole('dialog', { name: 'Card 1' })
    expect(dialog).toBeInTheDocument()
    expect(getImage().src).toBe('https://example.com/1.png')
  })
})

describe('CardLightbox – keyboard navigation', () => {
  it('ArrowLeft moves to the previous card', () => {
    render(<CardLightbox cards={fixture} startIndex={1} onClose={vi.fn()} />)
    fireEvent.keyDown(document, { key: 'ArrowLeft' })
    expect(getImage().src).toBe('https://example.com/0.png')
  })

  it('ArrowRight moves to the next card', () => {
    render(<CardLightbox cards={fixture} startIndex={1} onClose={vi.fn()} />)
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    expect(getImage().src).toBe('https://example.com/2.png')
  })

  it('Escape closes the lightbox', () => {
    const onClose = vi.fn()
    render(<CardLightbox cards={fixture} startIndex={1} onClose={onClose} />)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})

describe('CardLightbox – click navigation', () => {
  it('clicking the image closes the lightbox', () => {
    const onClose = vi.fn()
    render(<CardLightbox cards={fixture} startIndex={1} onClose={onClose} />)
    fireEvent.click(getImage())
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('clicking the left tap-zone goes to the previous card', () => {
    render(<CardLightbox cards={fixture} startIndex={1} onClose={vi.fn()} />)
    fireEvent.click(screen.getByTestId('lightbox-prev-zone'))
    expect(getImage().src).toBe('https://example.com/0.png')
  })

  it('clicking the right tap-zone goes to the next card', () => {
    render(<CardLightbox cards={fixture} startIndex={1} onClose={vi.fn()} />)
    fireEvent.click(screen.getByTestId('lightbox-next-zone'))
    expect(getImage().src).toBe('https://example.com/2.png')
  })

  it('clicking the explicit close button closes the lightbox', () => {
    const onClose = vi.fn()
    render(<CardLightbox cards={fixture} startIndex={0} onClose={onClose} />)
    fireEvent.click(screen.getByTestId('lightbox-close'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})

describe('CardLightbox – wrap-around', () => {
  it('past the last card wraps to the first', () => {
    render(<CardLightbox cards={fixture} startIndex={2} onClose={vi.fn()} />)
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    expect(getImage().src).toBe('https://example.com/0.png')
  })

  it('before the first card wraps to the last', () => {
    render(<CardLightbox cards={fixture} startIndex={0} onClose={vi.fn()} />)
    fireEvent.keyDown(document, { key: 'ArrowLeft' })
    expect(getImage().src).toBe('https://example.com/2.png')
  })
})

describe('CardLightbox – swipe', () => {
  it('a left swipe past the threshold advances to the next card', () => {
    vi.useFakeTimers()
    try {
      render(<CardLightbox cards={fixture} startIndex={1} onClose={vi.fn()} />)
      const overlay = screen.getByTestId('card-lightbox')

      fireEvent.touchStart(overlay, { touches: [{ clientX: 200, clientY: 100 }] })
      fireEvent.touchMove(overlay, { touches: [{ clientX: 100, clientY: 105 }] })
      fireEvent.touchEnd(overlay, { changedTouches: [{ clientX: 100, clientY: 105 }] })

      // Allow the post-swipe commit timer (150ms) to fire.
      act(() => {
        vi.advanceTimersByTime(200)
      })

      expect(getImage().src).toBe('https://example.com/2.png')
    } finally {
      vi.useRealTimers()
    }
  })

  it('a tiny horizontal drag below the threshold does nothing', () => {
    vi.useFakeTimers()
    try {
      render(<CardLightbox cards={fixture} startIndex={1} onClose={vi.fn()} />)
      const overlay = screen.getByTestId('card-lightbox')

      fireEvent.touchStart(overlay, { touches: [{ clientX: 200, clientY: 100 }] })
      fireEvent.touchMove(overlay, { touches: [{ clientX: 180, clientY: 100 }] })
      fireEvent.touchEnd(overlay, { changedTouches: [{ clientX: 180, clientY: 100 }] })

      act(() => {
        vi.advanceTimersByTime(200)
      })

      expect(getImage().src).toBe('https://example.com/1.png')
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('CardLightbox – metadata', () => {
  it('omits the price line when showPrice is false', () => {
    render(<CardLightbox cards={fixture} startIndex={0} onClose={vi.fn()} />)
    expect(screen.queryByTestId('lightbox-price')).not.toBeInTheDocument()
  })

  it('shows NOK and EUR when showPrice is true', () => {
    render(<CardLightbox cards={fixture} startIndex={0} onClose={vi.fn()} showPrice />)
    const price = screen.getByTestId('lightbox-price')
    // Number formatting depends on locale but both currency codes should appear.
    expect(price.textContent ?? '').toMatch(/NOK/i)
    expect(price.textContent ?? '').toMatch(/EUR|€/i)
  })
})

describe('CardLightbox – action bar slot', () => {
  it('renders the renderActionBar output and exposes the current card', () => {
    render(
      <CardLightbox
        cards={fixture}
        startIndex={1}
        onClose={vi.fn()}
        renderActionBar={(card) => <button type="button">action for {card.name}</button>}
      />,
    )
    expect(screen.getByRole('button', { name: 'action for Card 1' })).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    expect(screen.getByRole('button', { name: 'action for Card 2' })).toBeInTheDocument()
  })
})

describe('CardLightbox – body scroll lock', () => {
  it('locks body overflow on mount and restores on unmount', () => {
    document.body.style.overflow = ''
    const { unmount } = render(<CardLightbox cards={fixture} startIndex={0} onClose={vi.fn()} />)
    expect(document.body.style.overflow).toBe('hidden')
    unmount()
    expect(document.body.style.overflow).toBe('')
  })

  it('does not unlock body overflow when an outer lock is still active', () => {
    // Simulate an outer modal that locked the body before the lightbox
    // mounted; the lightbox must not stomp on that value when it closes.
    document.body.style.overflow = 'hidden'
    const { unmount } = render(<CardLightbox cards={fixture} startIndex={0} onClose={vi.fn()} />)
    expect(document.body.style.overflow).toBe('hidden')
    unmount()
    expect(document.body.style.overflow).toBe('hidden')
    document.body.style.overflow = ''
  })
})

describe('CardLightbox – cards shrink', () => {
  it('clamps currentIndex when the cards list shrinks past it', () => {
    const { rerender } = render(<CardLightbox cards={fixture} startIndex={2} onClose={vi.fn()} />)
    expect(getImage().src).toBe('https://example.com/2.png')

    rerender(<CardLightbox cards={fixture.slice(0, 1)} startIndex={2} onClose={vi.fn()} />)
    expect(getImage().src).toBe('https://example.com/0.png')
  })
})
