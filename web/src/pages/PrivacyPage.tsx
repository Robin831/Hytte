import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import { ArrowLeft } from 'lucide-react'

const SECTIONS = ['what', 'google', 'calendar', 'other', 'where', 'limited', 'rights', 'contact'] as const

/** Public privacy policy (linked from the welcome page and Google's sign-in screen). */
export default function PrivacyPage() {
  const { t } = useTranslation('privacy')
  return (
    <div className="mx-auto max-w-2xl p-4 py-8 sm:p-8">
      <Link to="/" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-200"><ArrowLeft size={16} aria-hidden="true" /> {t('back')}</Link>
      <h1 className="mt-4 text-3xl font-bold">{t('title')}</h1>
      <p className="mt-1 text-sm text-gray-500">{t('updated')}</p>
      {SECTIONS.map(s => (
        <section key={s} className="mt-6" aria-labelledby={`privacy-${s}`}>
          <h2 id={`privacy-${s}`} className="text-lg font-semibold">{t(`sections.${s}.title`)}</h2>
          <p className="mt-1 text-gray-300">{t(`sections.${s}.body`)}</p>
        </section>
      ))}
    </div>
  )
}
