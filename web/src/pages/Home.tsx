import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth'

function Home() {
  const { t } = useTranslation('common')
  const { user, loading } = useAuth()
  const [health, setHealth] = useState<string | null>(null)

  useEffect(() => {
    if (user) return
    fetch('/api/health')
      .then(res => res.json())
      .then(data => setHealth(data.status))
      .catch(() => setHealth('offline'))
  }, [user])

  if (loading) {
    return null
  }

  if (user) {
    return <Navigate to="/" replace />
  }

  return (
    <main className="flex items-center justify-center min-h-screen">
      <div className="text-center">
        <h1 className="text-6xl font-bold mb-4">{t('appName')}</h1>
        <p className="text-xl text-gray-400 mb-8">{t('tagline')}</p>
        <div className="inline-flex items-center gap-2 bg-gray-800 rounded-full px-4 py-2">
          <span
            className={`w-2 h-2 rounded-full ${
              health === null
                ? 'bg-gray-400 animate-pulse'
                : health === 'ok'
                  ? 'bg-green-500'
                  : 'bg-red-500'
            }`}
          />
          <span className="text-sm text-gray-300">
            {health === null ? t('status.checking') : t('api.label', { status: health })}
          </span>
        </div>
      </div>
    </main>
  )
}

export default Home
