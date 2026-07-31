import { useTranslation } from 'react-i18next'

/**
 * Full-screen cook mode: one step at a time, with per-step timers.
 *
 * Placeholder shell — the sibling sub-task for cook mode fills it in using the
 * `recipes:cook.*` keys and `useRecipe` from `hooks/useRecipes.ts`.
 */
export default function RecipeCookMode() {
  const { t } = useTranslation('recipes')

  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-2">{t('cook.title')}</h1>
      <p className="text-gray-400">{t('detail.loading')}</p>
    </div>
  )
}
