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
}

/** Cardmarket's wants list takes at most this many pasted lines at a time. */
export const WANTS_BLOCK_LINES = 150

/**
 * Lines for a Cardmarket wants list in the usual Pokémon decklist style:
 * "1 Meowth ex POR 121" (count, name, set code, number). Falls back to the
 * set name when the set code is unknown. Split into blocks Cardmarket
 * accepts in one paste.
 */
export function wantsListBlocks(cards: MissingCard[], setCodes: Map<string, string>, setNames: Map<string, string>): string[] {
  const lines = cards.map(c => {
    const set = setCodes.get(c.set_id) || setNames.get(c.set_id) || c.set_id
    return `1 ${c.name} ${set} ${c.collector_no}`
  })
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
