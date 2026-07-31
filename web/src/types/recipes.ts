/**
 * Shared recipe types and tag vocabularies.
 *
 * The shapes mirror the JSON the Go handlers in `internal/recipes` emit — see
 * `RecipeResponse`, `IngredientResponse` and `StepResponse` there. Timestamps
 * arrive as RFC3339 strings and stay strings on this side; callers format them
 * with `Intl.DateTimeFormat` using `i18n.language`.
 */

/** One line of a recipe's ingredient list. */
export interface RecipeIngredient {
  id: number
  position: number
  /** The full free-form line as the user typed it ("2 dl cream, room temperature"). */
  text: string
  /** Parsed amount, 0 when the line has no number. Scales with the portion control. */
  quantity: number
  unit: string
  name: string
}

/** One instruction in a recipe's method. */
export interface RecipeStep {
  id: number
  position: number
  text: string
  /** 0 when the step has no timed duration — cook mode only shows a timer above 0. */
  duration_seconds: number
}

/** A full recipe with its ingredients, steps and tags. */
export interface Recipe {
  id: number
  title: string
  notes: string
  /** The yield the stored quantities describe; 0 when the recipe declares none. */
  servings: number
  /** 1-5, null when the recipe has not been rated. */
  rating: number | null
  rated_at: string | null
  last_cooked_at: string | null
  created_at: string
  updated_at: string
  ingredients: RecipeIngredient[]
  steps: RecipeStep[]
  tags: string[]
}

/**
 * The list endpoint returns full recipes, so a summary is just a Recipe. The
 * alias exists so list-oriented components can name what they consume without
 * implying they need every field.
 */
export type RecipeSummary = Recipe

/** Query narrowing for the recipe list. */
export interface RecipeFilters {
  /** Client-side text match over title, ingredients and tags. */
  search?: string
  cuisine?: string
  season?: string
  occasion?: string
}

/** Payload for creating or updating a recipe (POST /api/recipes, PUT /api/recipes/{id}). */
export interface RecipeInput {
  title: string
  notes: string
  servings: number
  ingredients: Array<{ text: string; quantity: number; unit: string; name: string }>
  steps: Array<{ text: string; duration_seconds: number }>
  tags: string[]
}

/** Partial edit applied on top of the recipe currently loaded. */
export type RecipeUpdate = Partial<RecipeInput>

/** One item on the grocery list, as returned by the push endpoint. */
export interface GroceryItem {
  id: number
  household_id: number
  content: string
  original_text: string
  source_language: string
  checked: boolean
  sort_order: number
  added_by: number
  created_at: string
}

/** Body of POST /api/recipes/{id}/grocery. */
export interface GroceryPushRequest {
  recipeId: number
  /** IDs of the ingredients the user is missing. Must be non-empty. */
  ingredientIds: number[]
}

/** Result of a grocery push: what landed and what was already on the list. */
export interface GroceryPushResponse {
  added: number
  skipped: number
  items: GroceryItem[]
}

/**
 * Recipes carry free-form tags; these vocabularies are the subsets the list
 * filters expose. Each value doubles as the i18n key under
 * `recipes:filters.<group>Values.*`, so adding a value means adding the label
 * to all three locale files.
 */
export const CUISINE_TAGS = [
  'norwegian',
  'italian',
  'thai',
  'indian',
  'mexican',
  'japanese',
  'chinese',
  'french',
  'mediterranean',
  'american',
] as const

/**
 * Season tags match the seasons the backend ranks "cook again" by
 * (`seasonTags` in internal/recipes/store.go).
 */
export const SEASON_TAGS = ['winter', 'spring', 'summer', 'autumn'] as const

export const OCCASION_TAGS = [
  'weeknight',
  'weekend',
  'guests',
  'holiday',
  'breakfast',
  'lunch',
  'dinner',
  'dessert',
  'baking',
] as const

export type CuisineTag = (typeof CUISINE_TAGS)[number]
export type SeasonTag = (typeof SEASON_TAGS)[number]
export type OccasionTag = (typeof OCCASION_TAGS)[number]

/**
 * Scales an ingredient quantity from the recipe's own yield to the portions the
 * user picked. A recipe without a declared yield (or a line without a number)
 * cannot be scaled, so the quantity is returned untouched.
 */
export function scaleQuantity(quantity: number, servings: number, portions: number): number {
  if (servings <= 0 || portions <= 0) return quantity
  return (quantity * portions) / servings
}

/**
 * Denominators worth rendering as a fraction. Scaling lands on these far more
 * often than on a tidy decimal — a third of a recipe turns 1 egg into 0.333 —
 * and a cook reads "1 1/2 dl" faster than "1.5 dl".
 */
const FRACTION_DENOMINATORS = [2, 3, 4, 8]

/**
 * How far a value may sit from a whole number or a fraction and still be shown
 * as one. 0.01 is below the resolution of any kitchen measure, so 0.6667 reads
 * as "2/3" while 0.6 stays "0.6".
 */
const FRACTION_TOLERANCE = 0.01

/**
 * Formats a scaled quantity for display: whole numbers plain, near-fractions as
 * mixed numbers ("1 1/2"), anything else rounded to two decimals. A quantity of
 * 0 means the line states no amount, and renders as nothing at all.
 */
export function formatQuantity(quantity: number): string {
  if (!Number.isFinite(quantity) || quantity <= 0) return ''

  const rounded = Math.round(quantity)
  if (Math.abs(quantity - rounded) < FRACTION_TOLERANCE) return String(rounded)

  const whole = Math.floor(quantity)
  const remainder = quantity - whole
  for (const denominator of FRACTION_DENOMINATORS) {
    const numerator = Math.round(remainder * denominator)
    if (numerator <= 0 || numerator >= denominator) continue
    if (Math.abs(remainder - numerator / denominator) > FRACTION_TOLERANCE) continue
    const fraction = `${numerator}/${denominator}`
    return whole > 0 ? `${whole} ${fraction}` : fraction
  }

  return String(Math.round(quantity * 100) / 100)
}

/** Single-character fractions recipe sites write instead of "1/2". */
const UNICODE_FRACTIONS: Record<string, number> = {
  '½': 1 / 2,
  '⅓': 1 / 3,
  '⅔': 2 / 3,
  '¼': 1 / 4,
  '¾': 3 / 4,
  '⅕': 1 / 5,
  '⅖': 2 / 5,
  '⅗': 3 / 5,
  '⅘': 4 / 5,
  '⅙': 1 / 6,
  '⅚': 5 / 6,
  '⅛': 1 / 8,
  '⅜': 3 / 8,
  '⅝': 5 / 8,
  '⅞': 7 / 8,
}

const FRACTION_CHARS = Object.keys(UNICODE_FRACTIONS).join('')

/**
 * An amount as a written line spells it: "1 1/2", "1½", "3/4", "0,5", "2", "½".
 * The longer forms come first so "1 1/2" is not read as a bare "1".
 */
const AMOUNT_TOKEN =
  `\\d+\\s+\\d+/\\d+|\\d+\\s*[${FRACTION_CHARS}]|\\d+/\\d+|\\d+(?:[.,]\\d+)?|[${FRACTION_CHARS}]`

/** An amount, or a range of two ("2-3 dl"), anywhere in a written line. */
const WRITTEN_AMOUNT = new RegExp(
  `(${AMOUNT_TOKEN})(\\s*[-–—]\\s*)(${AMOUNT_TOKEN})|(${AMOUNT_TOKEN})`,
  'g',
)

/** Reads one written amount, or null when it is not a number after all. */
function parseAmountToken(token: string): number | null {
  const trimmed = token.trim()

  const fraction = UNICODE_FRACTIONS[trimmed.slice(-1)]
  if (fraction !== undefined) {
    const whole = trimmed.slice(0, -1).trim()
    if (whole === '') return fraction
    const parsed = Number(whole.replace(',', '.'))
    return Number.isFinite(parsed) ? parsed + fraction : null
  }

  let total = 0
  for (const part of trimmed.split(/\s+/)) {
    const slash = part.indexOf('/')
    if (slash >= 0) {
      const numerator = Number(part.slice(0, slash))
      const denominator = Number(part.slice(slash + 1))
      if (!Number.isFinite(numerator) || !Number.isFinite(denominator) || denominator === 0) {
        return null
      }
      total += numerator / denominator
      continue
    }
    const parsed = Number(part.replace(',', '.'))
    if (!Number.isFinite(parsed)) return null
    total += parsed
  }
  return total
}

/**
 * How far a written amount may sit from the parsed quantity and still be taken
 * for the same number — "1/3" is stored as 0.33 often enough to matter.
 */
function matchesQuantity(written: number, quantity: number): boolean {
  return Math.abs(written - quantity) <= Math.max(0.01, quantity * 0.02)
}

/**
 * Rewrites the amount inside a written ingredient line, leaving every other
 * word — unit, prep instruction, parenthetical — exactly where the line put it.
 * Returns null when the parsed quantity cannot be found in the line, in which
 * case the caller has to fall back to rebuilding the line from its parts.
 */
function rescaleWrittenAmount(text: string, quantity: number, factor: number): string | null {
  for (const match of text.matchAll(WRITTEN_AMOUNT)) {
    const [whole, low, separator, high, single] = match
    if (match.index === undefined) continue

    const first = parseAmountToken(low ?? single ?? '')
    if (first === null || !matchesQuantity(first, quantity)) continue

    const scaledLow = formatQuantity(first * factor)
    if (scaledLow === '') return null

    let replacement = scaledLow
    if (high !== undefined) {
      const second = parseAmountToken(high)
      if (second === null) return null
      const scaledHigh = formatQuantity(second * factor)
      if (scaledHigh === '') return null
      replacement += `${separator}${scaledHigh}`
    }

    return text.slice(0, match.index) + replacement + text.slice(match.index + whole.length)
  }
  return null
}

/**
 * How an ingredient should read at the chosen portion count.
 *
 * The written line is authoritative wherever it can be: it carries the prep and
 * parenthetical detail the parsed fields drop ("400 g cod, cubed"), so at the
 * recipe's own yield it is shown verbatim and when scaled only its amount is
 * rewritten in place. Rebuilding the line from amount/unit/name is the last
 * resort, for lines whose written amount cannot be located.
 */
export function ingredientLine(
  ingredient: RecipeIngredient,
  baseServings: number,
  portions: number,
): string {
  const { quantity, unit, name, text } = ingredient
  const written = text.trim()
  if (quantity <= 0) return written || name

  // scaleQuantity of 1 is the scaling factor itself, and is 1 whenever the
  // recipe cannot be scaled at all.
  const factor = scaleQuantity(1, baseServings, portions)
  if (written !== '') {
    if (factor === 1) return written
    const rescaled = rescaleWrittenAmount(written, quantity, factor)
    if (rescaled !== null) return rescaled
  }

  if (name === '') return written || name
  const amount = formatQuantity(quantity * factor)
  if (amount === '') return written || name
  return [amount, unit, name].filter(Boolean).join(' ')
}
