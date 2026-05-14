import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { Check, ChevronLeft, ChevronRight } from 'lucide-react'
import { formatNumber } from '../../utils/formatDate'

export interface LightboxVariant {
  price_eur?: number | null
  price_nok?: number | null
  owned?: boolean
}

export interface LightboxCard {
  id: string
  name: string
  collector_no: string
  set_id?: string
  set_name?: string
  image_large_url: string
  image_small_url?: string
  variants: LightboxVariant[]
}

export interface CardLightboxProps<TCard extends LightboxCard = LightboxCard> {
  cards: TCard[]
  startIndex: number
  onClose: () => void
  showPrice?: boolean
  renderActionBar?: (card: TCard, index: number) => ReactNode
}

const SWIPE_THRESHOLD_PX = 40
const SWIPE_MAX_VERTICAL_RATIO = Math.tan((30 * Math.PI) / 180)
const SWIPE_ANIMATE_MS = 150
const WRAP_FLASH_MS = 250
const CHEVRON_IDLE_MS = 1500

// Module-scoped lock so stacked lightboxes (or rapid mount/unmount during
// concurrent rendering) coordinate on body.style.overflow rather than each
// instance racing to capture/restore the value.
let bodyScrollLockCount = 0
let bodyScrollLockPrevious = ''

function lockBodyScroll() {
  if (bodyScrollLockCount === 0) {
    bodyScrollLockPrevious = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
  bodyScrollLockCount += 1
}

function unlockBodyScroll() {
  if (bodyScrollLockCount === 0) return
  bodyScrollLockCount -= 1
  if (bodyScrollLockCount === 0) {
    document.body.style.overflow = bodyScrollLockPrevious
    bodyScrollLockPrevious = ''
  }
}

function formatNok(amount: number | null | undefined): string {
  if (amount == null) return '—'
  return formatNumber(amount, {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

function formatEur(amount: number | null | undefined): string | null {
  if (amount == null) return null
  return formatNumber(amount, {
    style: 'currency',
    currency: 'EUR',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

function cardIsOwned(card: LightboxCard): boolean {
  return card.variants.some(v => v.owned)
}

function topVariant(card: LightboxCard): LightboxVariant | undefined {
  return card.variants[0]
}

function CardLightbox<TCard extends LightboxCard>({
  cards,
  startIndex,
  onClose,
  showPrice = false,
  renderActionBar,
}: CardLightboxProps<TCard>) {
  const { t } = useTranslation('pokemon')

  const safeStart = useMemo(() => {
    if (cards.length === 0) return 0
    if (startIndex < 0) return 0
    if (startIndex >= cards.length) return cards.length - 1
    return startIndex
  }, [cards.length, startIndex])

  const [currentIndex, setCurrentIndex] = useState(safeStart)
  const [wrapFlash, setWrapFlash] = useState<'start' | 'end' | null>(null)
  const [swipeOffset, setSwipeOffset] = useState(0)
  const [swipeAnimating, setSwipeAnimating] = useState(false)
  const [chevronsIdle, setChevronsIdle] = useState(false)
  const [announce, setAnnounce] = useState('')

  const dialogRef = useRef<HTMLDivElement>(null)
  const previousFocusRef = useRef<Element | null>(null)
  const touchStartRef = useRef<{ x: number; y: number } | null>(null)
  const swipeTimerRef = useRef<number | null>(null)

  const clampedIndex = cards.length === 0 ? 0 : Math.min(Math.max(0, currentIndex), cards.length - 1)
  const currentCard = cards[clampedIndex]

  const goPrev = useCallback(() => {
    if (cards.length === 0) return
    setChevronsIdle(false)
    setCurrentIndex(prev => {
      const safe = Math.min(Math.max(0, prev), cards.length - 1)
      if (safe <= 0) {
        setWrapFlash('end')
        setAnnounce(t('lightbox.wrappedToEnd'))
        return cards.length - 1
      }
      return safe - 1
    })
  }, [cards.length, t])

  const goNext = useCallback(() => {
    if (cards.length === 0) return
    setChevronsIdle(false)
    setCurrentIndex(prev => {
      const safe = Math.min(Math.max(0, prev), cards.length - 1)
      if (safe >= cards.length - 1) {
        setWrapFlash('start')
        setAnnounce(t('lightbox.wrappedToStart'))
        return 0
      }
      return safe + 1
    })
  }, [cards.length, t])

  // Body scroll lock — routed through a module-scoped counter so a parent
  // modal's lock isn't clobbered if this instance unmounts first.
  useEffect(() => {
    lockBodyScroll()
    return () => unlockBodyScroll()
  }, [])

  // Keyboard navigation. Capture-phase so we run before any underlying Dialog
  // listener (e.g. AddCardPanel's surrounding modal) and can stop Escape from
  // also closing that parent.
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopImmediatePropagation()
        onClose()
        return
      }
      if (e.key === 'ArrowLeft') {
        e.preventDefault()
        e.stopImmediatePropagation()
        goPrev()
        return
      }
      if (e.key === 'ArrowRight') {
        e.preventDefault()
        e.stopImmediatePropagation()
        goNext()
        return
      }
      if (e.key === 'Tab' && dialogRef.current) {
        const focusable = Array.from(
          dialogRef.current.querySelectorAll<HTMLElement>(
            'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
          ),
        )
        if (focusable.length === 0) return
        const first = focusable[0]
        const last = focusable[focusable.length - 1]
        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault()
            last.focus()
          }
        } else if (document.activeElement === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }
    document.addEventListener('keydown', handleKeyDown, true)
    return () => document.removeEventListener('keydown', handleKeyDown, true)
  }, [goNext, goPrev, onClose])

  // Focus the dialog on open, restore the previously focused element on close.
  useEffect(() => {
    previousFocusRef.current = document.activeElement
    dialogRef.current?.focus()
    return () => {
      if (previousFocusRef.current instanceof HTMLElement) {
        previousFocusRef.current.focus()
      }
    }
  }, [])

  // Clear the wrap flash after a short tick so it acts as a one-shot pulse.
  useEffect(() => {
    if (!wrapFlash) return
    const timer = setTimeout(() => setWrapFlash(null), WRAP_FLASH_MS)
    return () => clearTimeout(timer)
  }, [wrapFlash])

  // Fade the chevron hint after the user has had a moment to see it. Reset on
  // every navigation so the cue reappears with the new card.
  useEffect(() => {
    const timer = setTimeout(() => setChevronsIdle(true), CHEVRON_IDLE_MS)
    return () => clearTimeout(timer)
  }, [currentIndex])

  // Reset the announce live region after the screen reader has had a chance to
  // pick it up so the next wrap fires a fresh announcement.
  useEffect(() => {
    if (!announce) return
    const timer = setTimeout(() => setAnnounce(''), 1000)
    return () => clearTimeout(timer)
  }, [announce])

  const handleTouchStart = useCallback((e: React.TouchEvent) => {
    if (e.touches.length !== 1) return
    const touch = e.touches[0]
    touchStartRef.current = { x: touch.clientX, y: touch.clientY }
    setSwipeOffset(0)
    setSwipeAnimating(false)
  }, [])

  const handleTouchMove = useCallback((e: React.TouchEvent) => {
    const start = touchStartRef.current
    if (!start || e.touches.length !== 1) return
    const touch = e.touches[0]
    const dx = touch.clientX - start.x
    const dy = touch.clientY - start.y
    // Only follow finger if the gesture stays roughly horizontal — otherwise
    // it is probably a vertical scroll attempt.
    if (Math.abs(dy) > Math.abs(dx)) return
    setSwipeOffset(dx)
  }, [])

  const handleTouchEnd = useCallback((e: React.TouchEvent) => {
    const start = touchStartRef.current
    touchStartRef.current = null
    if (!start) return
    const last = e.changedTouches[0]
    if (!last) {
      setSwipeOffset(0)
      return
    }
    const dx = last.clientX - start.x
    const dy = last.clientY - start.y
    const horizontalEnough = Math.abs(dx) >= SWIPE_THRESHOLD_PX
    const dxSafe = Math.abs(dx) || 1
    const angleOk = Math.abs(dy) / dxSafe < SWIPE_MAX_VERTICAL_RATIO
    if (!horizontalEnough || !angleOk) {
      setSwipeOffset(0)
      return
    }
    const direction: 'next' | 'prev' = dx < 0 ? 'next' : 'prev'
    const viewportW = typeof window !== 'undefined' ? window.innerWidth : 800
    setSwipeAnimating(true)
    setSwipeOffset(direction === 'next' ? -viewportW : viewportW)
    if (swipeTimerRef.current !== null) {
      clearTimeout(swipeTimerRef.current)
    }
    swipeTimerRef.current = window.setTimeout(() => {
      swipeTimerRef.current = null
      if (direction === 'next') goNext()
      else goPrev()
      setSwipeAnimating(false)
      setSwipeOffset(0)
    }, SWIPE_ANIMATE_MS)
  }, [goNext, goPrev])

  // Cancel any pending swipe-commit on unmount so we don't run setState after
  // the component is gone.
  useEffect(() => {
    return () => {
      if (swipeTimerRef.current !== null) {
        clearTimeout(swipeTimerRef.current)
        swipeTimerRef.current = null
      }
    }
  }, [])

  if (cards.length === 0 || !currentCard) return null

  const owned = cardIsOwned(currentCard)
  const variant = topVariant(currentCard)
  const priceNok = variant?.price_nok ?? null
  const priceEur = variant?.price_eur ?? null
  const eurFormatted = formatEur(priceEur)

  const imageStyle = {
    transform: `translateX(${swipeOffset}px)`,
    transition: swipeAnimating ? `transform ${SWIPE_ANIMATE_MS}ms ease-out` : 'none',
  }

  const wrapFlashClass = wrapFlash
    ? wrapFlash === 'start'
      ? 'ring-4 ring-emerald-400/70'
      : 'ring-4 ring-amber-400/70'
    : ''

  const chevronOpacityClass = chevronsIdle
    ? 'opacity-0 sm:opacity-40'
    : 'opacity-40'

  const dialog = (
    <div
      className="fixed inset-0 z-[60] flex bg-black/95"
      style={{
        paddingTop: 'env(safe-area-inset-top)',
        paddingBottom: 'env(safe-area-inset-bottom)',
        paddingLeft: 'env(safe-area-inset-left)',
        paddingRight: 'env(safe-area-inset-right)',
      }}
      onTouchStart={handleTouchStart}
      onTouchMove={handleTouchMove}
      onTouchEnd={handleTouchEnd}
      data-testid="card-lightbox"
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={currentCard.name}
        tabIndex={-1}
        className="relative flex flex-1 items-stretch outline-none"
      >
        <button
          type="button"
          onClick={goPrev}
          aria-label={t('lightbox.previousCard')}
          data-testid="lightbox-prev-zone"
          className={`group flex w-[22%] min-w-[44px] items-center justify-start pl-2 cursor-pointer transition-opacity ${chevronOpacityClass} hover:!opacity-80 focus-visible:!opacity-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-400`}
        >
          <ChevronLeft size={48} className="text-white drop-shadow" aria-hidden="true" />
        </button>

        <div className="relative flex flex-1 flex-col items-center justify-center px-1 sm:px-2">
          <div className="flex flex-1 w-full items-center justify-center min-h-0">
            {currentCard.image_large_url ? (
              <img
                key={currentCard.id}
                src={currentCard.image_large_url}
                alt={currentCard.name}
                loading="eager"
                decoding="sync"
                onClick={onClose}
                style={imageStyle}
                data-testid="lightbox-image"
                className={`max-h-full max-w-full object-contain rounded shadow-2xl cursor-pointer ${wrapFlashClass}`}
              />
            ) : (
              <div className="text-gray-400" data-testid="lightbox-image-placeholder">
                {currentCard.collector_no}
              </div>
            )}
          </div>

          <div
            className="mt-2 sm:mt-3 w-full max-w-2xl px-3 py-2 text-center"
            data-testid="lightbox-meta"
          >
            <p className="text-base sm:text-lg font-semibold text-white truncate" title={currentCard.name}>
              {currentCard.name}
              {owned && (
                <span
                  aria-label={t('detail.ownership')}
                  className="ml-2 inline-flex items-center justify-center align-middle h-5 w-5 rounded-full bg-emerald-500 text-white"
                >
                  <Check size={12} aria-hidden="true" />
                </span>
              )}
            </p>
            <p className="text-xs sm:text-sm text-gray-300 mt-0.5">
              {currentCard.set_name ? `${currentCard.set_name} · ` : ''}
              {t('tile.collectorNo', { number: currentCard.collector_no })}
            </p>
            {showPrice && (
              <p className="text-xs sm:text-sm text-gray-200 mt-0.5" data-testid="lightbox-price">
                {formatNok(priceNok)}
                {eurFormatted ? ` (${eurFormatted})` : ''}
              </p>
            )}
          </div>

          {renderActionBar && (
            <div
              className="w-full max-w-2xl px-3 pb-2 pt-1"
              data-testid="lightbox-action-bar"
              onClick={e => e.stopPropagation()}
            >
              {renderActionBar(currentCard, clampedIndex)}
            </div>
          )}
        </div>

        <button
          type="button"
          onClick={goNext}
          aria-label={t('lightbox.nextCard')}
          data-testid="lightbox-next-zone"
          className={`group flex w-[22%] min-w-[44px] items-center justify-end pr-2 cursor-pointer transition-opacity ${chevronOpacityClass} hover:!opacity-80 focus-visible:!opacity-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-400`}
        >
          <ChevronRight size={48} className="text-white drop-shadow" aria-hidden="true" />
        </button>

        <button
          type="button"
          onClick={onClose}
          aria-label={t('lightbox.close')}
          data-testid="lightbox-close"
          className="absolute top-2 right-2 sm:top-4 sm:right-4 p-2 rounded-full bg-black/40 text-white hover:bg-black/70 cursor-pointer z-10"
        >
          <span aria-hidden="true" className="text-lg leading-none">×</span>
        </button>

        <div
          aria-live="polite"
          aria-atomic="true"
          className="sr-only"
          data-testid="lightbox-announce"
        >
          {announce}
        </div>
      </div>
    </div>
  )

  if (typeof document === 'undefined') return dialog
  return createPortal(dialog, document.body)
}

export default CardLightbox
