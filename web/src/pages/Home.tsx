import { useEffect, useState } from 'react'
import { useAuth } from '../auth'
import LoginButton from '../components/LoginButton'

function Home() {
  const { user, loading } = useAuth()
  const [health, setHealth] = useState<string>('checking...')

  useEffect(() => {
    fetch('/api/health')
      .then(res => res.json())
      .then(data => setHealth(data.status))
      .catch(() => setHealth('offline'))
  }, [])

  return (
    <main className="flex items-center justify-center" style={{ minHeight: 'calc(100vh - 72px)' }}>
      <div className="text-center">
        <h1 className="text-6xl font-bold mb-4">Hytte</h1>
        <p className="text-xl text-gray-400 mb-8">Your cozy corner of the web</p>
        <div className="inline-flex items-center gap-2 bg-gray-800 rounded-full px-4 py-2">
          <span className={`w-2 h-2 rounded-full ${health === 'ok' ? 'bg-green-500' : 'bg-red-500'}`} />
          <span className="text-sm text-gray-300">API: {health}</span>
        </div>
        {!loading && !user && (
          <div className="mt-8">
            <LoginButton />
          </div>
        )}
      </div>
    </main>
  )
}

export default Home
