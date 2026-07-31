import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type {
  GroceryPushRequest,
  GroceryPushResponse,
  Recipe,
  RecipeFilters,
  RecipeInput,
  RecipeUpdate,
} from '../types/recipes'

/**
 * Recipe data layer shared by the list, detail and cook-mode views.
 *
 * Every hook talks to the `/api/recipes` endpoints in `internal/recipes` with
 * `credentials: 'include'`, following the same fetch-in-useEffect pattern as
 * the other feature hooks in this directory (abort on unmount, error text from
 * the `recipes` namespace, loading reset in `finally`).
 */

/** Reads `{"error": "..."}` off a failed response, falling back to `fallback`. */
async function errorMessage(res: Response, fallback: string): Promise<string> {
  try {
    const data = await res.json()
    return typeof data?.error === 'string' ? data.error : fallback
  } catch {
    // Non-JSON body (proxy error page, empty 500) — the caller's text is better.
    return fallback
  }
}

/**
 * Builds the list query. Cuisine, season and occasion are ordinary recipe tags,
 * so they go out as repeated `tag_all` params: the backend then requires a
 * recipe to carry every selected tag. `search` is not a server-side parameter —
 * recipe text is encrypted at rest and cannot be matched in SQL — so it is
 * applied client-side by `matchesSearch`.
 */
function buildListQuery(filters: RecipeFilters): string {
  const params = new URLSearchParams()
  for (const tag of [filters.cuisine, filters.season, filters.occasion]) {
    if (tag) params.append('tag_all', tag)
  }
  const query = params.toString()
  return query ? `?${query}` : ''
}

/** Case-insensitive match over title, tags and ingredient lines. */
function matchesSearch(recipe: Recipe, search: string): boolean {
  const needle = search.trim().toLowerCase()
  if (!needle) return true
  if (recipe.title.toLowerCase().includes(needle)) return true
  if (recipe.tags.some(tag => tag.toLowerCase().includes(needle))) return true
  return recipe.ingredients.some(
    ing => ing.name.toLowerCase().includes(needle) || ing.text.toLowerCase().includes(needle),
  )
}

export interface UseRecipesResult {
  recipes: Recipe[]
  loading: boolean
  error: string
  /** Re-run the list fetch (after a create, delete or rating change). */
  refresh: () => void
}

/**
 * Lists the caller's recipes, newest first. Tag filters are applied by the
 * server; `filters.search` is applied here over the fetched page.
 */
export function useRecipes(filters: RecipeFilters = {}): UseRecipesResult {
  const { t } = useTranslation('recipes')
  const [recipes, setRecipes] = useState<Recipe[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refreshKey, setRefreshKey] = useState(0)

  const { search = '', cuisine, season, occasion } = filters
  // Depend on the query string rather than the filters object so a caller
  // rebuilding the object on every render does not re-fetch in a loop.
  const query = useMemo(
    () => buildListQuery({ cuisine, season, occasion }),
    [cuisine, season, occasion],
  )

  const refresh = useCallback(() => setRefreshKey(k => k + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      try {
        const res = await fetch(`/api/recipes${query}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(await errorMessage(res, t('errors.failedToLoad')))
        const data = await res.json()
        setRecipes(data.recipes ?? [])
        setError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [query, refreshKey, t])

  const filtered = useMemo(
    () => (search ? recipes.filter(recipe => matchesSearch(recipe, search)) : recipes),
    [recipes, search],
  )

  return { recipes: filtered, loading, error, refresh }
}

export interface UseCookAgainResult {
  recipes: Recipe[]
  loading: boolean
  error: string
  refresh: () => void
}

/**
 * Loads the "cook again" suggestions: recipes tagged for the current season,
 * ranked by how long it has been since they were last made. `limit` is passed
 * straight through to the endpoint's own bounds check.
 */
export function useCookAgain(limit?: number): UseCookAgainResult {
  const { t } = useTranslation('recipes')
  const [recipes, setRecipes] = useState<Recipe[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refreshKey, setRefreshKey] = useState(0)

  const refresh = useCallback(() => setRefreshKey(k => k + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      try {
        const query = limit != null ? `?limit=${limit}` : ''
        const res = await fetch(`/api/recipes/cook-again${query}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(await errorMessage(res, t('errors.failedToLoad')))
        const data = await res.json()
        setRecipes(data.recipes ?? [])
        setError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [limit, refreshKey, t])

  return { recipes, loading, error, refresh }
}

export interface UseRecipeResult {
  recipe: Recipe | null
  loading: boolean
  error: string
  /** True when the server answered 404 — the caller shows "not found", not an error banner. */
  notFound: boolean
  refresh: () => void
}

/**
 * Loads a single recipe with its ingredients, steps and tags. `id` may be the
 * raw route param; a non-numeric value is treated as "not found" without a
 * request.
 */
export function useRecipe(id: string | number | undefined): UseRecipeResult {
  const { t } = useTranslation('recipes')
  const [recipe, setRecipe] = useState<Recipe | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [notFound, setNotFound] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)

  const refresh = useCallback(() => setRefreshKey(k => k + 1), [])

  useEffect(() => {
    const numericID = Number(id)
    if (id === undefined || id === '' || !Number.isFinite(numericID)) {
      setRecipe(null)
      setNotFound(true)
      setLoading(false)
      return
    }

    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      try {
        const res = await fetch(`/api/recipes/${numericID}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (res.status === 404) {
          setRecipe(null)
          setNotFound(true)
          setError('')
          return
        }
        if (!res.ok) throw new Error(await errorMessage(res, t('errors.failedToLoadRecipe')))
        setRecipe(await res.json())
        setNotFound(false)
        setError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [id, refreshKey, t])

  return { recipe, loading, error, notFound, refresh }
}

export interface UseUpdateRecipeResult {
  /**
   * Applies `changes` on top of `current` and PUTs the whole recipe — the
   * endpoint replaces ingredients, steps and tags wholesale, so a partial body
   * would drop the fields it omits. Returns the saved recipe, or null on failure.
   */
  update: (current: Recipe, changes: RecipeUpdate) => Promise<Recipe | null>
  saving: boolean
  error: string
  clearError: () => void
}

/** Mutation hook for PUT /api/recipes/{id}. */
export function useUpdateRecipe(): UseUpdateRecipeResult {
  const { t } = useTranslation('recipes')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const clearError = useCallback(() => setError(''), [])

  const update = useCallback(async (current: Recipe, changes: RecipeUpdate): Promise<Recipe | null> => {
    setSaving(true)
    setError('')
    const body: RecipeInput = {
      title: changes.title ?? current.title,
      notes: changes.notes ?? current.notes,
      servings: changes.servings ?? current.servings,
      ingredients: changes.ingredients ?? current.ingredients.map(ing => ({
        text: ing.text,
        quantity: ing.quantity,
        unit: ing.unit,
        name: ing.name,
      })),
      steps: changes.steps ?? current.steps.map(step => ({
        text: step.text,
        duration_seconds: step.duration_seconds,
      })),
      tags: changes.tags ?? current.tags,
    }
    try {
      const res = await fetch(`/api/recipes/${current.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) throw new Error(await errorMessage(res, t('errors.failedToSave')))
      return (await res.json()) as Recipe
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToSave'))
      return null
    } finally {
      setSaving(false)
    }
  }, [t])

  return { update, saving, error, clearError }
}

export interface UsePushMissingToGroceryResult {
  /** Pushes the selected ingredients onto the grocery list. Returns null on failure. */
  push: (request: GroceryPushRequest) => Promise<GroceryPushResponse | null>
  pushing: boolean
  error: string
  clearError: () => void
}

/** Mutation hook for POST /api/recipes/{id}/grocery. */
export function usePushMissingToGrocery(): UsePushMissingToGroceryResult {
  const { t } = useTranslation('recipes')
  const [pushing, setPushing] = useState(false)
  const [error, setError] = useState('')

  const clearError = useCallback(() => setError(''), [])

  const push = useCallback(async ({ recipeId, ingredientIds }: GroceryPushRequest) => {
    setPushing(true)
    setError('')
    try {
      const res = await fetch(`/api/recipes/${recipeId}/grocery`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ingredient_ids: ingredientIds }),
      })
      if (!res.ok) throw new Error(await errorMessage(res, t('detail.addMissingError')))
      return (await res.json()) as GroceryPushResponse
    } catch (err) {
      setError(err instanceof Error ? err.message : t('detail.addMissingError'))
      return null
    } finally {
      setPushing(false)
    }
  }, [t])

  return { push, pushing, error, clearError }
}
