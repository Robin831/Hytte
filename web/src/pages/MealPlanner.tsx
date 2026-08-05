import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import { Check, ChevronLeft, ChevronRight, Plus, Search, Star, Trash2, X } from 'lucide-react'
import { formatDate } from '../utils/formatDate'
import {
  matchesSearch,
  usePlanActions,
  usePlanWeek,
  useRecipeActions,
  useRecipes,
} from '../hooks/useRecipes'
import {
  MEAL_SLOTS,
  addPlanDays,
  isoWeekStart,
  parsePlanDate,
  toPlanDate,
  type MealSlot,
  type PlanEntry,
  type PlanWeek,
  type Recipe,
} from '../types/recipes'

/**
 * Weekly meal planner: seven day cards, each holding up to one recipe per meal
 * slot, with a cook-and-rate flow per planned meal.
 *
 * Assignment is tap/select throughout — tapping a day opens a recipe picker,
 * tapping a recipe fills the slot. There is deliberately no drag-and-drop: it
 * needs a pointer library, does not survive a 375px touch screen well, and
 * would be the only interaction on the page a keyboard could not drive.
 *
 * Nothing lives only in component state. Every assign and clear goes through
 * the plan endpoints and the week is re-fetched on mount, so the plan is the
 * same after a reload; the local `setWeek` calls below are optimistic previews
 * that roll back when the request fails.
 */

/** Entries of one day in eating order, whatever order they arrived in. */
function sortedEntries(entries: PlanEntry[]): PlanEntry[] {
  return [...entries].sort((a, b) => MEAL_SLOTS.indexOf(a.slot) - MEAL_SLOTS.indexOf(b.slot))
}

/** The week with `entry` filed under its day, replacing whatever held that slot. */
function withEntry(week: PlanWeek, entry: PlanEntry): PlanWeek {
  const day = week.days[entry.date] ?? []
  return {
    ...week,
    days: { ...week.days, [entry.date]: [...day.filter(e => e.slot !== entry.slot), entry] },
  }
}

/** The week with one (day, slot) emptied. */
function withoutEntry(week: PlanWeek, date: string, slot: MealSlot): PlanWeek {
  return {
    ...week,
    days: { ...week.days, [date]: (week.days[date] ?? []).filter(e => e.slot !== slot) },
  }
}

/**
 * ID for an entry that only exists locally, while its PUT is in flight. Real
 * IDs come from the database and are positive, so a negative one can never
 * collide — and it never reaches the server, which is told (date, slot, recipe).
 */
const PENDING_ENTRY_ID = -1

const OVERLAY_CLASS =
  'fixed inset-0 z-50 flex items-end justify-center bg-black/70 sm:items-center sm:p-4'

/** Sheet on a phone (bottom, full width), centred dialog from `sm:` up. */
const SHEET_CLASS =
  'flex w-full max-h-[85vh] flex-col rounded-t-2xl border border-gray-700 bg-gray-900 sm:max-w-md sm:rounded-2xl'

const BUTTON_CLASS =
  'flex items-center gap-2 rounded-lg px-3 py-2 min-h-11 text-sm transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed'

export default function MealPlanner() {
  const { t } = useTranslation('recipes')

  // The client normalises to Monday itself, so the seven day cards below match
  // the week the server files the entries under.
  const [weekStart, setWeekStart] = useState(() => isoWeekStart(new Date()))
  const { week, setWeek, loading, error, refresh } = usePlanWeek(weekStart)

  // Fetched unfiltered so the picker opens on an already-loaded list and the
  // search box narrows it without a round trip.
  const { recipes, loading: recipesLoading } = useRecipes()
  const plan = usePlanActions()
  const actions = useRecipeActions()

  const [picker, setPicker] = useState<{ date: string; slot: MealSlot } | null>(null)
  const [pickerSearch, setPickerSearch] = useState('')
  const [rating, setRating] = useState<{ recipeId: number; title: string } | null>(null)
  /** Plan entries logged as cooked in this session — the plan row itself has no cooked flag. */
  const [cooked, setCooked] = useState<number[]>([])

  const dates = useMemo(
    () => Array.from({ length: 7 }, (_, index) => addPlanDays(weekStart, index)),
    [weekStart],
  )
  const today = toPlanDate(new Date())
  const plannedCount = dates.reduce((total, date) => total + (week?.days[date]?.length ?? 0), 0)

  const pickerRecipes = useMemo(
    () => recipes.filter(recipe => matchesSearch(recipe, pickerSearch)),
    [recipes, pickerSearch],
  )

  function openPicker(date: string, slot: MealSlot) {
    setPickerSearch('')
    plan.clearError()
    setPicker({ date, slot })
  }

  async function assign(recipe: Recipe) {
    if (!picker || !week) return
    const { date, slot } = picker
    const previous = week

    setPicker(null)
    setWeek(current =>
      current
        ? withEntry(current, {
            id: PENDING_ENTRY_ID,
            date,
            slot,
            recipe_id: recipe.id,
            recipe_title: recipe.title,
          })
        : current,
    )

    const saved = await plan.assign(date, slot, recipe.id)
    if (!saved) {
      setWeek(previous)
      return
    }
    setWeek(current => (current ? withEntry(current, saved) : current))
  }

  async function remove(entry: PlanEntry) {
    if (!week) return
    const previous = week

    setWeek(current => (current ? withoutEntry(current, entry.date, entry.slot) : current))
    if (!(await plan.clear(entry.date, entry.slot))) {
      setWeek(previous)
    }
  }

  /**
   * Logs the cook, then asks for a rating: both feed the "cook again" ranking,
   * which orders by season and by how long ago something was last made.
   */
  async function markCooked(entry: PlanEntry) {
    actions.clearError()
    const updated = await actions.logCooked(entry.recipe_id)
    if (!updated) return

    setCooked(ids => (ids.includes(entry.id) ? ids : [...ids, entry.id]))
    setRating({ recipeId: entry.recipe_id, title: entry.recipe_title })
  }

  async function submitRating(value: number) {
    if (!rating) return
    if (await actions.rate(rating.recipeId, value)) {
      setRating(null)
    }
  }

  const banner = error || plan.error || actions.error

  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      <Link to="/recipes" className="text-sm text-blue-400 hover:text-blue-300">
        {t('detail.back')}
      </Link>

      <h1 className="mt-2 text-xl font-semibold">{t('planner.title')}</h1>
      <p className="text-sm text-gray-400">{t('planner.subtitle')}</p>

      {/* Week switcher: arrows either side of the range, "this week" only when
          the user has navigated away from it. */}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => setWeekStart(current => addPlanDays(current, -7))}
          aria-label={t('planner.previousWeek')}
          className={`${BUTTON_CLASS} bg-gray-800 hover:bg-gray-700`}
        >
          <ChevronLeft size={16} aria-hidden="true" />
        </button>
        <div className="min-w-0 flex-1 text-center">
          <p className="font-medium break-words">
            {t('planner.weekRange', {
              start: formatDate(parsePlanDate(weekStart), { day: 'numeric', month: 'short' }),
              end: formatDate(parsePlanDate(dates[6]), {
                day: 'numeric',
                month: 'short',
                year: 'numeric',
              }),
            })}
          </p>
          <p className="text-sm text-gray-400">{t('planner.mealCount', { count: plannedCount })}</p>
        </div>
        <button
          type="button"
          onClick={() => setWeekStart(current => addPlanDays(current, 7))}
          aria-label={t('planner.nextWeek')}
          className={`${BUTTON_CLASS} bg-gray-800 hover:bg-gray-700`}
        >
          <ChevronRight size={16} aria-hidden="true" />
        </button>
        {weekStart !== isoWeekStart(new Date()) && (
          <button
            type="button"
            onClick={() => setWeekStart(isoWeekStart(new Date()))}
            className={`${BUTTON_CLASS} w-full justify-center bg-gray-800 hover:bg-gray-700 sm:w-auto`}
          >
            {t('planner.thisWeek')}
          </button>
        )}
      </div>

      {banner && (
        <div className="mt-4 flex flex-wrap items-center gap-3 rounded-lg border border-red-800 bg-red-900/50 px-4 py-2 text-sm text-red-300">
          <span className="min-w-0 break-words">{banner}</span>
          <button
            type="button"
            onClick={refresh}
            className="cursor-pointer underline hover:text-red-200"
          >
            {t('errors.retry')}
          </button>
        </div>
      )}

      {loading && !week ? (
        <p role="status" aria-busy="true" className="mt-6 text-gray-400">
          {t('planner.loading')}
        </p>
      ) : (
        <div className="mt-4 space-y-3">
          {dates.map(date => (
            <DayCard
              key={date}
              date={date}
              isToday={date === today}
              entries={sortedEntries(week?.days[date] ?? [])}
              cooked={cooked}
              busy={plan.busy || actions.busy}
              onAdd={slot => openPicker(date, slot)}
              onCook={markCooked}
              onRemove={remove}
            />
          ))}
        </div>
      )}

      {picker && (
        <RecipePicker
          date={picker.date}
          slot={picker.slot}
          recipes={pickerRecipes}
          loading={recipesLoading}
          hasRecipes={recipes.length > 0}
          search={pickerSearch}
          onSearch={setPickerSearch}
          onSlot={slot => setPicker(current => (current ? { ...current, slot } : current))}
          onPick={assign}
          onClose={() => setPicker(null)}
        />
      )}

      {rating && (
        <RatingPrompt
          title={rating.title}
          busy={actions.busy}
          onRate={submitRating}
          onSkip={() => setRating(null)}
        />
      )}
    </div>
  )
}

/**
 * One day of the week: what is planned, and the way to plan more. Slots without
 * an entry are not rendered as empty rows — four of those per day would be 28
 * rows of nothing on a phone — they are reachable through "add a meal".
 */
function DayCard({
  date,
  isToday,
  entries,
  cooked,
  busy,
  onAdd,
  onCook,
  onRemove,
}: {
  date: string
  isToday: boolean
  entries: PlanEntry[]
  cooked: number[]
  busy: boolean
  onAdd: (slot: MealSlot) => void
  onCook: (entry: PlanEntry) => void
  onRemove: (entry: PlanEntry) => void
}) {
  const { t } = useTranslation('recipes')
  const day = parsePlanDate(date)
  const heading = `${formatDate(day, { weekday: 'long' })} ${formatDate(day, { day: 'numeric', month: 'short' })}`
  // Which slot "add a meal" opens on. Dinner is what a week gets planned around,
  // so it is the default whenever it is free; otherwise the next free slot in
  // eating order, and dinner again for a day with nothing free (assigning then
  // replaces it, which is what the endpoint does anyway).
  const taken = new Set(entries.map(entry => entry.slot))
  const nextSlot = !taken.has('dinner')
    ? 'dinner'
    : (MEAL_SLOTS.find(slot => !taken.has(slot)) ?? 'dinner')

  return (
    <section
      aria-label={heading}
      className={`rounded-lg bg-gray-800 p-3 ${isToday ? 'ring-1 ring-blue-500' : ''}`}
    >
      <div className="flex flex-wrap items-baseline gap-2">
        <h2 className="font-medium">{heading}</h2>
        {isToday && <span className="text-xs uppercase text-blue-400">{t('planner.today')}</span>}
      </div>

      {entries.length === 0 ? (
        <p className="mt-1 text-sm text-gray-500">{t('planner.emptyDay')}</p>
      ) : (
        <ul className="mt-2 space-y-2">
          {entries.map(entry => (
            <li key={entry.slot} className="rounded-lg bg-gray-900 p-2">
              <p className="text-xs uppercase tracking-wide text-gray-500">
                {t(`planner.slots.${entry.slot}`)}
              </p>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <Link
                  to={`/recipes/${entry.recipe_id}`}
                  className="min-w-0 break-words text-blue-300 hover:text-blue-200"
                >
                  {entry.recipe_title}
                </Link>
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => onCook(entry)}
                    className={`${BUTTON_CLASS} ${
                      cooked.includes(entry.id)
                        ? 'text-green-300 bg-gray-800'
                        : 'bg-gray-800 hover:bg-gray-700'
                    }`}
                  >
                    <Check size={16} aria-hidden="true" />
                    {cooked.includes(entry.id) ? t('planner.cooked') : t('planner.markCooked')}
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => onRemove(entry)}
                    aria-label={t('planner.remove', { title: entry.recipe_title })}
                    className={`${BUTTON_CLASS} bg-gray-800 text-red-300 hover:bg-gray-700`}
                  >
                    <Trash2 size={16} aria-hidden="true" />
                  </button>
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}

      <button
        type="button"
        onClick={() => onAdd(nextSlot)}
        className={`${BUTTON_CLASS} mt-2 bg-gray-700 hover:bg-gray-600`}
      >
        <Plus size={16} aria-hidden="true" />
        {t('planner.addMeal', { day: formatDate(day, { weekday: 'long' }) })}
      </button>
    </section>
  )
}

/**
 * Recipe picker: pick the slot, then tap a recipe. It is a bottom sheet on a
 * phone and a centred dialog from `sm:` up; the list scrolls inside the panel
 * so the page behind it never grows.
 */
function RecipePicker({
  date,
  slot,
  recipes,
  loading,
  hasRecipes,
  search,
  onSearch,
  onSlot,
  onPick,
  onClose,
}: {
  date: string
  slot: MealSlot
  recipes: Recipe[]
  loading: boolean
  /** Whether the user has any recipes at all — an empty filter result reads differently. */
  hasRecipes: boolean
  search: string
  onSearch: (value: string) => void
  onSlot: (slot: MealSlot) => void
  onPick: (recipe: Recipe) => void
  onClose: () => void
}) {
  const { t } = useTranslation('recipes')
  const day = formatDate(parsePlanDate(date), { weekday: 'long', day: 'numeric', month: 'short' })

  return (
    <div className={OVERLAY_CLASS}>
      <div role="dialog" aria-modal="true" aria-labelledby="planner-picker-title" className={SHEET_CLASS}>
        <div className="flex items-start justify-between gap-2 border-b border-gray-800 p-3">
          <div className="min-w-0">
            <h2 id="planner-picker-title" className="font-semibold">
              {t('planner.pickerTitle')}
            </h2>
            <p className="text-sm text-gray-400 break-words">
              {t('planner.pickerFor', { slot: t(`planner.slots.${slot}`), day })}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={t('planner.close')}
            className={`${BUTTON_CLASS} bg-gray-800 hover:bg-gray-700`}
          >
            <X size={16} aria-hidden="true" />
          </button>
        </div>

        <div className="space-y-2 border-b border-gray-800 p-3">
          <div role="group" aria-label={t('planner.slot')} className="flex flex-nowrap gap-2 overflow-x-auto py-1">
            {MEAL_SLOTS.map(value => (
              <button
                key={value}
                type="button"
                aria-pressed={value === slot}
                onClick={() => onSlot(value)}
                className={`shrink-0 rounded-full px-3 py-1 min-h-11 text-sm transition-colors cursor-pointer ${
                  value === slot ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-300 hover:bg-gray-700'
                }`}
              >
                {t(`planner.slots.${value}`)}
              </button>
            ))}
          </div>
          <div className="relative">
            <Search
              size={16}
              aria-hidden="true"
              className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500"
            />
            <input
              type="search"
              value={search}
              onChange={e => onSearch(e.target.value)}
              aria-label={t('planner.pickerSearch')}
              placeholder={t('planner.pickerSearchPlaceholder')}
              className="w-full min-h-11 rounded-lg border border-gray-700 bg-gray-800 py-2 pl-9 pr-3 text-sm"
            />
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-3">
          {loading ? (
            <p role="status" aria-busy="true" className="text-gray-400">
              {t('list.loading')}
            </p>
          ) : recipes.length === 0 ? (
            <p className="py-6 text-center text-gray-500">
              {hasRecipes ? t('planner.pickerEmpty') : t('planner.pickerNoRecipes')}
            </p>
          ) : (
            <ul className="space-y-2">
              {recipes.map(recipe => (
                <li key={recipe.id}>
                  <button
                    type="button"
                    onClick={() => onPick(recipe)}
                    className="w-full min-h-11 cursor-pointer rounded-lg bg-gray-800 px-3 py-2 text-left transition-colors hover:bg-gray-700"
                  >
                    <span className="break-words">{recipe.title}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  )
}

/**
 * Asked for straight after a cook is logged. The score is what makes "cook
 * again" useful, so it is prompted for at the one moment the user has an
 * opinion — but skipping is one tap, and the cook is already recorded either way.
 */
function RatingPrompt({
  title,
  busy,
  onRate,
  onSkip,
}: {
  title: string
  busy: boolean
  onRate: (value: number) => void
  onSkip: () => void
}) {
  const { t } = useTranslation('recipes')

  return (
    <div className={OVERLAY_CLASS}>
      <div role="dialog" aria-modal="true" aria-labelledby="planner-rating-title" className={SHEET_CLASS}>
        <div className="p-4">
          <h2 id="planner-rating-title" className="font-semibold break-words">
            {t('planner.ratePrompt', { title })}
          </h2>
          <p className="mt-1 text-sm text-gray-400">{t('planner.rateHint')}</p>

          <div
            role="group"
            aria-label={t('rating.label')}
            className="mt-3 flex items-center justify-center gap-1"
          >
            {[1, 2, 3, 4, 5].map(star => (
              <button
                key={star}
                type="button"
                disabled={busy}
                onClick={() => onRate(star)}
                aria-label={t('rating.star', { value: star })}
                className="flex h-11 w-11 cursor-pointer items-center justify-center disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Star size={24} aria-hidden="true" className="text-amber-400" />
              </button>
            ))}
          </div>

          <button
            type="button"
            onClick={onSkip}
            className={`${BUTTON_CLASS} mt-3 w-full justify-center bg-gray-800 hover:bg-gray-700`}
          >
            {t('planner.rateSkip')}
          </button>
        </div>
      </div>
    </div>
  )
}
