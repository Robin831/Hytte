import { useCallback, useEffect, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  ArrowLeft,
  Check,
  ChevronDown,
  ChevronUp,
  Loader2,
  Minus,
  Pencil,
  Plus,
  ShoppingCart,
  Star,
  Trash2,
  Utensils,
  X,
} from 'lucide-react'
import { formatDate } from '../utils/formatDate'
import {
  useCreateRecipe,
  usePushMissingToGrocery,
  useRecipe,
  useRecipeActions,
  useUpdateRecipe,
} from '../hooks/useRecipes'
import {
  ingredientLine,
  type GroceryPushResponse,
  type Recipe,
  type RecipeInput,
} from '../types/recipes'

/**
 * One recipe: ingredients, method, a portion control and the grocery push.
 *
 * The recipe loaded from the server is the single source of truth for
 * quantities. `portions` only ever changes what is *rendered* — every displayed
 * amount is `scaleQuantity(base, recipe.servings, portions)` computed at render
 * time, and the edit form binds to the base quantities, so saving from a scaled
 * view writes the recipe exactly as it was stored.
 *
 * The route is also used in create mode (`/recipes/new`), which the list page
 * navigates to directly and after an import; it opens the same editor over an
 * empty (or imported, unsaved) recipe and POSTs it.
 */

// --- draft model ---
//
// The editor keeps numbers as strings so a half-typed or cleared field stays
// exactly what the user typed instead of collapsing to NaN or 0. `fromDraft`
// converts back at save time.

/** Row identity for the editor's lists — the API IDs are absent for new rows. */
let draftKeySeed = 0

function nextDraftKey(): string {
  draftKeySeed += 1
  return `draft-${draftKeySeed}`
}

interface DraftIngredient {
  key: string
  /** The free-form line as written ("400 g cod, cubed"). */
  text: string
  /** Base quantity — never the scaled one shown by the portion control. */
  quantity: string
  unit: string
  /** The bare ingredient, which is what a grocery push puts on the list. */
  name: string
}

interface DraftStep {
  key: string
  text: string
  /** Steps are timed in whole minutes here; the API stores seconds. */
  minutes: string
}

interface Draft {
  title: string
  notes: string
  servings: string
  ingredients: DraftIngredient[]
  steps: DraftStep[]
  tags: string[]
}

function emptyDraft(): Draft {
  return {
    title: '',
    notes: '',
    servings: '',
    ingredients: [blankIngredient()],
    steps: [blankStep()],
    tags: [],
  }
}

function blankIngredient(): DraftIngredient {
  return { key: nextDraftKey(), text: '', quantity: '', unit: '', name: '' }
}

function blankStep(): DraftStep {
  return { key: nextDraftKey(), text: '', minutes: '' }
}

/** Copies a recipe into an editable draft. The recipe itself is never mutated. */
function toDraft(recipe: Recipe): Draft {
  return {
    title: recipe.title,
    notes: recipe.notes,
    servings: recipe.servings > 0 ? String(recipe.servings) : '',
    ingredients: recipe.ingredients.map(ing => ({
      key: nextDraftKey(),
      text: ing.text,
      quantity: ing.quantity > 0 ? String(ing.quantity) : '',
      unit: ing.unit,
      name: ing.name,
    })),
    steps: recipe.steps.map(step => ({
      key: nextDraftKey(),
      text: step.text,
      // Sub-minute durations are rounded to the nearest minute when a step is
      // opened in the editor, and saved back at that resolution.
      minutes: step.duration_seconds > 0 ? String(Math.round(step.duration_seconds / 60)) : '',
    })),
    tags: [...recipe.tags],
  }
}

/** Reads a draft's numeric field, treating anything unparseable as 0. */
function toNumber(value: string): number {
  const parsed = Number(value.replace(',', '.'))
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

/**
 * Builds the API payload from a draft. Quantities come straight off the draft,
 * which holds base amounts, so the portion control cannot leak into a save.
 * Blank rows are dropped — the backend drops them too, and doing it here keeps
 * the saved recipe and the form in agreement.
 */
function fromDraft(draft: Draft): RecipeInput {
  return {
    title: draft.title.trim(),
    notes: draft.notes.trim(),
    servings: Math.round(toNumber(draft.servings)),
    ingredients: draft.ingredients
      .filter(ing => ing.text.trim() !== '' || ing.name.trim() !== '')
      .map(ing => ({
        text: ing.text.trim() || composeLine(ing),
        quantity: toNumber(ing.quantity),
        unit: ing.unit.trim(),
        name: ing.name.trim(),
      })),
    steps: draft.steps
      .filter(step => step.text.trim() !== '')
      .map(step => ({
        text: step.text.trim(),
        duration_seconds: Math.round(toNumber(step.minutes) * 60),
      })),
    tags: draft.tags,
  }
}

/** Free-form line for a row the user filled in by parts only. */
function composeLine(ing: DraftIngredient): string {
  return [ing.quantity.trim(), ing.unit.trim(), ing.name.trim()].filter(Boolean).join(' ')
}

// --- display helpers ---

/** Total timed minutes across the method, or 0 when no step declares a duration. */
function totalMinutes(recipe: Recipe): number {
  const seconds = recipe.steps.reduce((sum, step) => sum + Math.max(0, step.duration_seconds), 0)
  return Math.round(seconds / 60)
}

// --- page ---

export default function RecipeDetail() {
  const { t } = useTranslation('recipes')
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const location = useLocation()

  const isCreate = id === 'new'
  const importState = location.state as { importedRecipe?: Recipe; importUrl?: string } | null
  const importedRecipe = importState?.importedRecipe ?? null
  const importUrl = importState?.importUrl ?? ''

  const { recipe, loading, error, notFound, refresh } = useRecipe(isCreate ? undefined : id)
  const { create, saving: creating, error: createError } = useCreateRecipe()
  const { update, saving: updating, error: updateError } = useUpdateRecipe()
  const actions = useRecipeActions()
  const { push, pushing, error: pushError, clearError: clearPushError } = usePushMissingToGrocery()

  // A non-null draft means the editor is open. Create mode opens straight into
  // it, over the imported recipe when the list page handed one over.
  const [draft, setDraft] = useState<Draft | null>(() =>
    isCreate ? (importedRecipe ? toDraft(importedRecipe) : emptyDraft()) : null,
  )
  const [formError, setFormError] = useState('')
  // null means "whatever the recipe yields". Deriving rather than seeding state
  // from the fetch keeps the very first render with a recipe already showing
  // the stored quantities instead of briefly scaling them by zero.
  const [portionsOverride, setPortionsOverride] = useState<number | null>(null)
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [pushResult, setPushResult] = useState<GroceryPushResponse | null>(null)
  const [cookLogged, setCookLogged] = useState(false)

  const recipeId = recipe?.id
  const baseServings = recipe?.servings ?? 0
  const portions = portionsOverride ?? baseServings

  // Re-anchor the portion control when an edit changes the yield it scales
  // from. A plain refresh (rating, cook log) leaves the user's choice alone.
  useEffect(() => {
    setPortionsOverride(null)
  }, [recipeId, baseServings])

  useEffect(() => {
    setSelectedIds(new Set())
    setPushResult(null)
    setCookLogged(false)
  }, [recipeId])

  const toggleIngredient = useCallback((ingredientId: number) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(ingredientId)) next.delete(ingredientId)
      else next.add(ingredientId)
      return next
    })
  }, [])

  const allSelected =
    recipe != null && recipe.ingredients.length > 0 && selectedIds.size === recipe.ingredients.length

  async function saveDraft() {
    if (!draft) return
    const input = fromDraft(draft)
    if (input.title === '') {
      setFormError(t('edit.titleRequired'))
      return
    }
    setFormError('')

    if (isCreate) {
      const created = await create(input)
      if (!created) return
      setDraft(null)
      navigate(`/recipes/${created.id}`, { replace: true })
      return
    }

    if (!recipe) return
    const saved = await update(recipe, input)
    if (!saved) return
    setDraft(null)
    refresh()
  }

  async function markCooked() {
    if (!recipe) return
    const updated = await actions.logCooked(recipe.id)
    if (!updated) return
    setCookLogged(true)
    refresh()
  }

  async function setRating(value: number) {
    if (!recipe) return
    const updated = await actions.rate(recipe.id, value)
    if (updated) refresh()
  }

  async function deleteRecipe() {
    if (!recipe) return
    if (!window.confirm(t('detail.deleteConfirm'))) return
    if (await actions.remove(recipe.id)) navigate('/recipes')
  }

  async function pushSelected() {
    if (!recipe) return
    // Sent in the recipe's own ingredient order so the grocery list reads the
    // way the recipe does.
    const ingredientIds = recipe.ingredients
      .filter(ing => selectedIds.has(ing.id))
      .map(ing => ing.id)
    if (ingredientIds.length === 0) return

    setPushResult(null)
    clearPushError()
    const result = await push({ recipeId: recipe.id, ingredientIds })
    if (!result) return
    setPushResult(result)
    setSelectedIds(new Set())
  }

  const backLink = (
    <Link
      to="/recipes"
      className="inline-flex items-center gap-2 text-sm text-gray-400 hover:text-gray-200"
    >
      <ArrowLeft size={16} aria-hidden="true" />
      {t('detail.back')}
    </Link>
  )

  if (!isCreate && loading) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-6">
        {backLink}
        <p role="status" aria-busy="true" className="mt-4 text-gray-400">
          {t('detail.loading')}
        </p>
      </div>
    )
  }

  if (!isCreate && notFound) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-6">
        {backLink}
        <p className="mt-4 text-gray-400">{t('errors.notFound')}</p>
      </div>
    )
  }

  if (!isCreate && !recipe) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-6">
        {backLink}
        <div className="mt-4 px-4 py-2 bg-red-900/50 border border-red-800 rounded-lg text-red-300 text-sm">
          {error || t('errors.failedToLoadRecipe')}
        </div>
        <button
          type="button"
          onClick={refresh}
          className="mt-3 px-4 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm"
        >
          {t('errors.retry')}
        </button>
      </div>
    )
  }

  const saveError = isCreate ? createError : updateError
  const saving = isCreate ? creating : updating

  if (draft) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-6">
        {backLink}
        <h1 className="mt-3 mb-1 text-xl font-semibold">
          {isCreate ? t('create.title') : t('detail.edit')}
        </h1>
        {isCreate && importUrl !== '' && (
          <p className="mb-3 text-sm text-gray-400">{t('create.imported', { url: importUrl })}</p>
        )}
        <RecipeEditor
          draft={draft}
          onChange={setDraft}
          onSave={saveDraft}
          onCancel={() => {
            setFormError('')
            if (isCreate) navigate('/recipes')
            else setDraft(null)
          }}
          saving={saving}
          error={formError || saveError}
        />
      </div>
    )
  }

  // Past this point the recipe is loaded: create mode always has a draft open.
  if (!recipe) return null

  const total = totalMinutes(recipe)

  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      {backLink}

      <div className="mt-3 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold break-words">{recipe.title}</h1>
          <p className="text-sm text-gray-400">
            {recipe.last_cooked_at
              ? t('list.lastCooked', {
                  date: formatDate(recipe.last_cooked_at, {
                    year: 'numeric',
                    month: 'short',
                    day: 'numeric',
                  }),
                })
              : t('list.neverCooked')}
            {total > 0 && ` · ${t('detail.totalTime', { minutes: total })}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => {
              setFormError('')
              setDraft(toDraft(recipe))
            }}
            className="flex items-center gap-2 px-3 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm"
          >
            <Pencil size={16} aria-hidden="true" />
            {t('detail.edit')}
          </button>
          <button
            type="button"
            onClick={deleteRecipe}
            disabled={actions.busy}
            className="flex items-center gap-2 px-3 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm text-red-300 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Trash2 size={16} aria-hidden="true" />
            {t('detail.delete')}
          </button>
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-3">
        <RatingPicker value={recipe.rating} disabled={actions.busy} onRate={setRating} />
        <button
          type="button"
          onClick={markCooked}
          disabled={actions.busy}
          className="flex items-center gap-2 px-3 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Check size={16} aria-hidden="true" />
          {cookLogged ? t('detail.markedCooked') : t('detail.markCooked')}
        </button>
        <Link
          to={`/recipes/${recipe.id}/cook`}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors text-sm"
        >
          <Utensils size={16} aria-hidden="true" />
          {t('detail.startCooking')}
        </Link>
      </div>

      {actions.error && (
        <div className="mt-3 px-4 py-2 bg-red-900/50 border border-red-800 rounded-lg text-red-300 text-sm">
          {actions.error}
        </div>
      )}

      {recipe.tags.length > 0 && (
        <ul className="mt-3 flex flex-wrap gap-1" aria-label={t('detail.tags')}>
          {recipe.tags.map(tag => (
            <li key={tag} className="px-2 py-0.5 text-xs rounded-full bg-gray-700 text-gray-300">
              {tag}
            </li>
          ))}
        </ul>
      )}

      {baseServings > 0 && (
        <PortionControl
          portions={portions}
          baseServings={baseServings}
          onChange={setPortionsOverride}
        />
      )}

      <section aria-labelledby="ingredients-heading" className="mt-6">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 id="ingredients-heading" className="text-lg font-semibold">
            {t('detail.ingredients')}
          </h2>
          {recipe.ingredients.length > 0 && (
            <button
              type="button"
              onClick={() =>
                setSelectedIds(
                  allSelected ? new Set() : new Set(recipe.ingredients.map(ing => ing.id)),
                )
              }
              className="text-sm text-blue-400 hover:text-blue-300 cursor-pointer"
            >
              {allSelected ? t('detail.selectNone') : t('detail.selectAll')}
            </button>
          )}
        </div>

        {recipe.ingredients.length === 0 ? (
          <p className="mt-2 text-sm text-gray-500">{t('detail.noIngredients')}</p>
        ) : (
          <ul className="mt-2 divide-y divide-gray-800">
            {recipe.ingredients.map(ing => {
              const line = ingredientLine(ing, baseServings, portions)
              return (
                <li key={ing.id}>
                  <label className="flex items-start gap-3 py-3 min-h-11 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={selectedIds.has(ing.id)}
                      onChange={() => toggleIngredient(ing.id)}
                      aria-label={`${t('detail.selectIngredient')}: ${line}`}
                      className="mt-0.5 size-5 shrink-0 accent-blue-500"
                    />
                    <span className="min-w-0 break-words">{line}</span>
                  </label>
                </li>
              )
            })}
          </ul>
        )}

        {recipe.ingredients.length > 0 && (
          <div className="mt-3">
            <button
              type="button"
              onClick={pushSelected}
              disabled={pushing || selectedIds.size === 0}
              className="flex w-full sm:w-auto items-center justify-center gap-2 px-4 py-3 min-h-11 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer text-sm disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {pushing ? (
                <Loader2 size={16} aria-hidden="true" className="animate-spin" />
              ) : (
                <ShoppingCart size={16} aria-hidden="true" />
              )}
              {pushing ? t('detail.addMissingPending') : t('detail.addMissingToGroceryList')}
            </button>
            {selectedIds.size === 0 && !pushing && (
              <p className="mt-2 text-sm text-gray-500">{t('detail.addMissingEmpty')}</p>
            )}
            {pushError && (
              <div className="mt-2 px-4 py-2 bg-red-900/50 border border-red-800 rounded-lg text-red-300 text-sm">
                {pushError}
              </div>
            )}
            {pushResult && (
              <p role="status" className="mt-2 text-sm text-green-300">
                {t('detail.addMissingSuccess', { count: pushResult.added })}
                {pushResult.skipped > 0 &&
                  ` · ${t('detail.addMissingSkipped', { count: pushResult.skipped })}`}
              </p>
            )}
          </div>
        )}
      </section>

      <section aria-labelledby="steps-heading" className="mt-6">
        <h2 id="steps-heading" className="text-lg font-semibold">
          {t('detail.steps')}
        </h2>
        {recipe.steps.length === 0 ? (
          <p className="mt-2 text-sm text-gray-500">{t('detail.noSteps')}</p>
        ) : (
          <ol className="mt-2 space-y-3">
            {recipe.steps.map((step, index) => (
              <li key={step.id} className="flex gap-3">
                <span className="shrink-0 w-7 h-7 rounded-full bg-gray-800 text-sm flex items-center justify-center">
                  {index + 1}
                </span>
                <div className="min-w-0">
                  <p className="break-words whitespace-pre-wrap">{step.text}</p>
                  {step.duration_seconds > 0 && (
                    <p className="text-sm text-gray-500">
                      {t('detail.stepDuration', { minutes: Math.round(step.duration_seconds / 60) })}
                    </p>
                  )}
                </div>
              </li>
            ))}
          </ol>
        )}
      </section>

      {recipe.notes.trim() !== '' && (
        <section aria-labelledby="notes-heading" className="mt-6">
          <h2 id="notes-heading" className="text-lg font-semibold">
            {t('detail.notes')}
          </h2>
          <p className="mt-2 break-words whitespace-pre-wrap text-gray-300">{recipe.notes}</p>
        </section>
      )}
    </div>
  )
}

// --- portion control ---

/**
 * Chooses how many portions the ingredient list should read for. It never
 * touches the recipe — the caller only uses `portions` as a render-time
 * multiplier.
 */
function PortionControl({
  portions,
  baseServings,
  onChange,
}: {
  portions: number
  baseServings: number
  onChange: (portions: number) => void
}) {
  const { t } = useTranslation('recipes')

  return (
    <div className="mt-4 flex flex-wrap items-center gap-3 bg-gray-800 rounded-lg p-3">
      <span className="text-sm text-gray-300">{t('detail.portions')}</span>
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => onChange(Math.max(1, portions - 1))}
          disabled={portions <= 1}
          aria-label={t('detail.portionDecrease')}
          className="w-11 h-11 flex items-center justify-center bg-gray-700 hover:bg-gray-600 rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Minus size={16} aria-hidden="true" />
        </button>
        <input
          type="number"
          min={1}
          value={portions}
          onChange={e => onChange(Math.max(1, Math.round(Number(e.target.value) || 1)))}
          aria-label={t('detail.portions')}
          className="w-16 h-11 px-2 text-center bg-gray-900 border border-gray-700 rounded-lg"
        />
        <button
          type="button"
          onClick={() => onChange(portions + 1)}
          aria-label={t('detail.portionIncrease')}
          className="w-11 h-11 flex items-center justify-center bg-gray-700 hover:bg-gray-600 rounded-lg transition-colors cursor-pointer"
        >
          <Plus size={16} aria-hidden="true" />
        </button>
      </div>
      {portions !== baseServings && (
        <div className="flex flex-wrap items-center gap-2 text-sm text-gray-400">
          <span>{t('detail.scaledFrom', { count: baseServings })}</span>
          <button
            type="button"
            onClick={() => onChange(baseServings)}
            className="text-blue-400 hover:text-blue-300 cursor-pointer"
          >
            {t('detail.portionReset', { count: baseServings })}
          </button>
        </div>
      )}
    </div>
  )
}

// --- rating ---

/** Five clickable stars; clicking the current rating again clears it. */
function RatingPicker({
  value,
  disabled,
  onRate,
}: {
  value: number | null
  disabled: boolean
  onRate: (value: number) => void
}) {
  const { t } = useTranslation('recipes')

  return (
    <div role="group" aria-label={t('rating.label')} className="flex items-center gap-1">
      {[1, 2, 3, 4, 5].map(star => (
        <button
          key={star}
          type="button"
          disabled={disabled}
          onClick={() => onRate(value === star ? 0 : star)}
          aria-label={value === star ? t('rating.clear') : t('rating.star', { value: star })}
          aria-pressed={value != null && star <= value}
          className="w-11 h-11 flex items-center justify-center cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Star
            size={20}
            aria-hidden="true"
            className={
              value != null && star <= value ? 'text-amber-400 fill-amber-400' : 'text-gray-600'
            }
          />
        </button>
      ))}
      <span className="text-sm text-gray-400">
        {value == null ? t('rating.none') : t('rating.value', { value })}
      </span>
    </div>
  )
}

// --- editor ---

const INPUT_BASE = 'min-w-0 px-3 py-2 min-h-11 bg-gray-900 border border-gray-700 rounded-lg text-sm'
const INPUT_CLASS = `w-full ${INPUT_BASE}`

/**
 * Form over a draft recipe. Every quantity input binds to the base amount, so
 * the portion control in the view above has no effect on what gets saved.
 */
function RecipeEditor({
  draft,
  onChange,
  onSave,
  onCancel,
  saving,
  error,
}: {
  draft: Draft
  onChange: (draft: Draft) => void
  onSave: () => void
  onCancel: () => void
  saving: boolean
  error: string
}) {
  const { t } = useTranslation('recipes')
  const [tagInput, setTagInput] = useState('')

  function patch(changes: Partial<Draft>) {
    onChange({ ...draft, ...changes })
  }

  function patchIngredient(index: number, changes: Partial<DraftIngredient>) {
    patch({
      ingredients: draft.ingredients.map((ing, i) => (i === index ? { ...ing, ...changes } : ing)),
    })
  }

  function patchStep(index: number, changes: Partial<DraftStep>) {
    patch({ steps: draft.steps.map((step, i) => (i === index ? { ...step, ...changes } : step)) })
  }

  function moveStep(index: number, delta: number) {
    const target = index + delta
    if (target < 0 || target >= draft.steps.length) return
    const steps = [...draft.steps]
    ;[steps[index], steps[target]] = [steps[target], steps[index]]
    patch({ steps })
  }

  function addTag() {
    const tag = tagInput.trim().toLowerCase()
    if (tag === '' || draft.tags.includes(tag)) {
      setTagInput('')
      return
    }
    patch({ tags: [...draft.tags, tag] })
    setTagInput('')
  }

  return (
    <form
      onSubmit={e => {
        e.preventDefault()
        onSave()
      }}
      className="space-y-6"
    >
      <div className="space-y-3">
        <label className="block">
          <span className="block mb-1 text-sm text-gray-400">{t('edit.titleField')}</span>
          <input
            type="text"
            value={draft.title}
            onChange={e => patch({ title: e.target.value })}
            className={INPUT_CLASS}
          />
        </label>
        <div>
          <label className="block">
            <span className="block mb-1 text-sm text-gray-400">{t('edit.servings')}</span>
            <input
              type="number"
              min={0}
              value={draft.servings}
              onChange={e => patch({ servings: e.target.value })}
              className={`${INPUT_CLASS} sm:w-32`}
            />
          </label>
          <p className="mt-1 text-xs text-gray-500">{t('edit.baseQuantitiesHint')}</p>
        </div>
      </div>

      <fieldset>
        <legend className="text-lg font-semibold">{t('edit.tags')}</legend>
        {draft.tags.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-2">
            {draft.tags.map(tag => (
              <span
                key={tag}
                className="flex items-center gap-1 pl-3 pr-1 py-1 text-sm rounded-full bg-gray-700"
              >
                {tag}
                <button
                  type="button"
                  onClick={() => patch({ tags: draft.tags.filter(other => other !== tag) })}
                  aria-label={t('edit.removeTag', { tag })}
                  className="w-7 h-7 flex items-center justify-center rounded-full hover:bg-gray-600 cursor-pointer"
                >
                  <X size={14} aria-hidden="true" />
                </button>
              </span>
            ))}
          </div>
        )}
        <div className="mt-2 flex gap-2">
          <input
            type="text"
            value={tagInput}
            onChange={e => setTagInput(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addTag()
              }
            }}
            aria-label={t('edit.tags')}
            placeholder={t('edit.addTagPlaceholder')}
            className={INPUT_CLASS}
          />
          <button
            type="button"
            onClick={addTag}
            className="shrink-0 px-4 min-h-11 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm"
          >
            {t('edit.addTag')}
          </button>
        </div>
      </fieldset>

      <fieldset>
        <legend className="text-lg font-semibold">{t('edit.ingredients')}</legend>
        <ul className="mt-2 space-y-4">
          {draft.ingredients.map((ing, index) => (
            <li key={ing.key} className="bg-gray-800 rounded-lg p-3 space-y-2">
              <div className="flex items-start gap-2">
                <input
                  type="text"
                  value={ing.text}
                  onChange={e => patchIngredient(index, { text: e.target.value })}
                  aria-label={t('edit.ingredientText', { index: index + 1 })}
                  className={INPUT_CLASS}
                />
                <button
                  type="button"
                  onClick={() =>
                    patch({ ingredients: draft.ingredients.filter((_, i) => i !== index) })
                  }
                  aria-label={t('edit.removeIngredient', { index: index + 1 })}
                  className="shrink-0 w-11 h-11 flex items-center justify-center bg-gray-700 hover:bg-gray-600 rounded-lg transition-colors cursor-pointer"
                >
                  <Trash2 size={16} aria-hidden="true" />
                </button>
              </div>
              {/* Quantity, unit and name are the parsed triple: the quantity is
                  what scaling multiplies and the name is what a grocery push
                  puts on the list, so both stay editable next to the raw line. */}
              <div className="grid grid-cols-3 gap-2">
                <input
                  type="number"
                  min={0}
                  step="any"
                  value={ing.quantity}
                  onChange={e => patchIngredient(index, { quantity: e.target.value })}
                  aria-label={t('edit.ingredientQuantity', { index: index + 1 })}
                  className={INPUT_CLASS}
                />
                <input
                  type="text"
                  value={ing.unit}
                  onChange={e => patchIngredient(index, { unit: e.target.value })}
                  aria-label={t('edit.ingredientUnit', { index: index + 1 })}
                  className={INPUT_CLASS}
                />
                <input
                  type="text"
                  value={ing.name}
                  onChange={e => patchIngredient(index, { name: e.target.value })}
                  aria-label={t('edit.ingredientName', { index: index + 1 })}
                  className={INPUT_CLASS}
                />
              </div>
            </li>
          ))}
        </ul>
        <button
          type="button"
          onClick={() => patch({ ingredients: [...draft.ingredients, blankIngredient()] })}
          className="mt-2 flex items-center gap-2 px-3 py-2 min-h-11 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm"
        >
          <Plus size={16} aria-hidden="true" />
          {t('edit.addIngredient')}
        </button>
      </fieldset>

      <fieldset>
        <legend className="text-lg font-semibold">{t('edit.steps')}</legend>
        <ol className="mt-2 space-y-4">
          {draft.steps.map((step, index) => (
            <li key={step.key} className="bg-gray-800 rounded-lg p-3 space-y-2">
              <textarea
                value={step.text}
                onChange={e => patchStep(index, { text: e.target.value })}
                aria-label={t('edit.stepText', { index: index + 1 })}
                rows={2}
                className={INPUT_CLASS}
              />
              <div className="flex flex-wrap items-center gap-2">
                <input
                  type="number"
                  min={0}
                  value={step.minutes}
                  onChange={e => patchStep(index, { minutes: e.target.value })}
                  aria-label={t('edit.stepMinutes', { index: index + 1 })}
                  className={`${INPUT_BASE} w-24`}
                />
                <button
                  type="button"
                  onClick={() => moveStep(index, -1)}
                  disabled={index === 0}
                  aria-label={t('edit.moveStepUp', { index: index + 1 })}
                  className="w-11 h-11 flex items-center justify-center bg-gray-700 hover:bg-gray-600 rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <ChevronUp size={16} aria-hidden="true" />
                </button>
                <button
                  type="button"
                  onClick={() => moveStep(index, 1)}
                  disabled={index === draft.steps.length - 1}
                  aria-label={t('edit.moveStepDown', { index: index + 1 })}
                  className="w-11 h-11 flex items-center justify-center bg-gray-700 hover:bg-gray-600 rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <ChevronDown size={16} aria-hidden="true" />
                </button>
                <button
                  type="button"
                  onClick={() => patch({ steps: draft.steps.filter((_, i) => i !== index) })}
                  aria-label={t('edit.removeStep', { index: index + 1 })}
                  className="w-11 h-11 flex items-center justify-center bg-gray-700 hover:bg-gray-600 rounded-lg transition-colors cursor-pointer"
                >
                  <Trash2 size={16} aria-hidden="true" />
                </button>
              </div>
            </li>
          ))}
        </ol>
        <button
          type="button"
          onClick={() => patch({ steps: [...draft.steps, blankStep()] })}
          className="mt-2 flex items-center gap-2 px-3 py-2 min-h-11 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm"
        >
          <Plus size={16} aria-hidden="true" />
          {t('edit.addStep')}
        </button>
      </fieldset>

      <label className="block">
        <span className="block mb-1 text-sm text-gray-400">{t('edit.notes')}</span>
        <textarea
          value={draft.notes}
          onChange={e => patch({ notes: e.target.value })}
          rows={4}
          className={INPUT_CLASS}
        />
      </label>

      {error && (
        <div className="px-4 py-2 bg-red-900/50 border border-red-800 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      <div className="flex flex-wrap gap-2">
        <button
          type="submit"
          disabled={saving}
          className="flex items-center gap-2 px-4 py-2 min-h-11 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer text-sm disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {saving && <Loader2 size={16} aria-hidden="true" className="animate-spin" />}
          {saving ? t('edit.saving') : t('edit.save')}
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="px-4 py-2 min-h-11 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm"
        >
          {t('edit.cancel')}
        </button>
      </div>
    </form>
  )
}
