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

/** The cheapest in-stock offer, if any. Offers arrive sorted in-stock first, then by price. */
export function bestOffer(card: MissingCard): Offer | undefined {
  const first = card.offers[0]
  return first && first.available ? first : undefined
}
