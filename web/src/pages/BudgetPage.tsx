import { Link } from 'react-router-dom'
import { Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export default function BudgetPage() {
  const { t } = useTranslation('budget')

  return (
    <div className="p-6 max-w-2xl mx-auto">
      <h1 className="text-2xl font-semibold text-white mb-6">{t('title')}</h1>

      <div className="grid gap-4">
        <Link
          to="/budget/import"
          className="flex items-center gap-4 p-4 rounded-lg bg-gray-800 border border-gray-700 hover:border-blue-500 transition-colors group"
        >
          <div className="p-2 rounded-lg bg-blue-500/10 text-blue-400 group-hover:bg-blue-500/20">
            <Upload size={24} />
          </div>
          <div>
            <div className="text-white font-medium">{t('import.title')}</div>
            <div className="text-sm text-gray-400">{t('import.description')}</div>
          </div>
        </Link>
      </div>
    </div>
  )
}
