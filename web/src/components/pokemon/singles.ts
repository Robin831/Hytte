// Types and helpers for the "Buy singles" page (internal/pokemon/singles.go).

export interface Offer {
  shop_id: number
  shop_name: string
  variant_id: string
  print: string
  condition: string
  price_nok: number
  available: boolean
  url: string
  copies?: number
}

export interface MissingCard {
  id: string
  set_id: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  cardmarket_eur: number
  cardmarket_nok: number | null
  cardmarket_url: string
  offers: Offer[]
  /** Set once the card is ordered; it waits in "Ordered" until scanned in. */
  ordered?: { source: string; ordered_at: string } | null
}

/** Cardmarket's wants list takes at most this many pasted lines at a time. */
export const WANTS_BLOCK_LINES = 150

/** Splits wants-list lines into blocks Cardmarket accepts in one paste. */
export function wantsListBlocks(lines: string[]): string[] {
  const blocks: string[] = []
  for (let i = 0; i < lines.length; i += WANTS_BLOCK_LINES) {
    blocks.push(lines.slice(i, i + WANTS_BLOCK_LINES).join('\n'))
  }
  return blocks
}

/** The cheapest in-stock offer, if any. Offers arrive sorted in-stock first, then by price. */
export function bestOffer(card: MissingCard): Offer | undefined {
  const first = card.offers[0]
  return first && first.available ? first : undefined
}
