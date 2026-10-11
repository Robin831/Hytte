import { useTranslation } from 'react-i18next'
import { LogIn } from 'lucide-react'
import { useAuth } from '../auth'
import Dashboard from './Dashboard'
import WeatherWidget from '../components/widgets/WeatherWidget'

/** What a visitor sees before signing in: the name, a way in, and the weather. */
function Welcome() {
  const { t } = useTranslation('common')
  return (
    <div className="mx-auto flex max-w-md flex-col gap-6 p-4 py-10 sm:p-6 sm:py-16">
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
    </div>
  )
}

/** The landing page: your dashboard when signed in, a welcome when not. */
export default function HomePage() {
  const { user, loading } = useAuth()
  if (loading) return null
  return user ? <Dashboard /> : <Welcome />
}
