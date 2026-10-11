import { Link } from 'react-router'
import { BookOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'

/** A child's shortcut into the homework helper. */
export default function HomeworkWidget() {
  const { t } = useTranslation('dashboard')
  return (
    <Widget title={t('widgets.homework.title')}>
      <p className="text-sm text-gray-300">{t('widgets.homework.prompt')}</p>
      <Link
        to="/homework"
        className="mt-3 inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500"
      >
        <BookOpen size={16} aria-hidden="true" />
        {t('widgets.homework.open')}
      </Link>
    </Widget>
  )
}
