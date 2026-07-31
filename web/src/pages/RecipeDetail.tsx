import { useTranslation } from 'react-i18next'

/**
 * Single recipe: ingredients, method, portion control and the grocery push.
 *
 * Placeholder shell — the sibling sub-task for the detail view fills it in on
 * top of `useRecipe`, `useUpdateRecipe` and `usePushMissingToGrocery` from
 * `hooks/useRecipes.ts`.
 */
export default function RecipeDetail() {
  const { t } = useTranslation('recipes')

  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-2">{t('list.title')}</h1>
      <p className="text-gray-400">{t('detail.loading')}</p>
    </div>
  )
}
