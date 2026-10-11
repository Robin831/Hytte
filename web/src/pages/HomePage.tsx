import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router'
import { LogIn } from 'lucide-react'
import { useAuth } from '../auth'
import Dashboard from './Dashboard'
import WeatherWidget from '../components/widgets/WeatherWidget'

/** What a visitor sees before signing in: the name, a way in, and the weather. */
function Welcome() {
  const { t } = useTranslation('common')
  const [params] = useSearchParams()
  const notInvited = params.get('error') === 'not_invited'
  return (
    <div className="mx-auto flex max-w-md flex-col gap-6 p-4 py-10 sm:p-6 sm:py-16">
      {notInvited && (
        <p role="alert" className="rounded-lg border border-amber-700 bg-amber-900/30 p-4 text-sm text-amber-100">{t('notInvited')}</p>
      )}
      <header className="text-center">
        <h1 className="text-5xl font-bold">{t('appName')}</h1>
        <p className="mt-3 text-lg text-gray-400">{t('tagline')}</p>
        <a
          href="/api/auth/google/login"
          className="mt-6 inline-flex items-center gap-2 rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-medium text-white transition-colors hover:bg-blue-500"
        >
          <LogIn size={16} aria-hidden="true" />
          {t('sidebar.signIn')}
        </a>
      </header>
      <WeatherWidget />
      <p className="text-center text-xs text-gray-500"><Link to="/privacy" className="hover:text-gray-300">{t('sidebar.privacy')}</Link></p>
    </div>
  )
}

/** The landing page: your dashboard when signed in, a welcome when not. */
export default function HomePage() {
  const { user, loading } = useAuth()
  if (loading) return null
  return user ? <Dashboard /> : <Welcome />
}
