import { useTranslation } from 'react-i18next'

/**
 * Recipe list.
 *
 * Placeholder shell: the route, the `recipes` i18n namespace and the data layer
 * in `hooks/useRecipes.ts` land with this scaffolding, and the sibling sub-task
 * for the list view fills in filters, the "cook again" row and the recipe cards.
 */
export default function RecipesPage() {
  const { t } = useTranslation('recipes')

  return (
    <div className="max-w-4xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-2">{t('list.title')}</h1>
      <p className="text-gray-400">{t('list.subtitle')}</p>
    </div>
  )
}
