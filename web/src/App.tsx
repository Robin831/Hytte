import { useEffect, useState } from 'react'
import { AuthProvider } from './auth/AuthContext.tsx'
import { useAuth } from './auth/useAuth.ts'
import { LoginButton } from './components/LoginButton.tsx'
import { ProfileDropdown } from './components/ProfileDropdown.tsx'

function AppContent() {
  const [health, setHealth] = useState<string>('checking...')
  const { user, loading } = useAuth()

  useEffect(() => {
    fetch('/api/health')
      .then(res => res.json())
      .then(data => setHealth(data.status))
      .catch(() => setHealth('offline'))
  }, [])

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <header className="flex items-center justify-between px-6 py-4">
        <h2 className="text-lg font-semibold">Hytte</h2>
        <div>
          {loading ? null : user ? <ProfileDropdown /> : <LoginButton />}
        </div>
      </header>

      <main className="flex flex-1 items-center justify-center" style={{ minHeight: 'calc(100vh - 72px)' }}>
        <div className="text-center">
          <h1 className="text-6xl font-bold mb-4">Hytte</h1>
          <p className="text-xl text-gray-400 mb-8">Your cozy corner of the web</p>
          {user && (
            <p className="text-lg text-gray-300 mb-6">
              Welcome back, {user.name}!
            </p>
          )}
          <div className="inline-flex items-center gap-2 bg-gray-800 rounded-full px-4 py-2">
            <span className={`w-2 h-2 rounded-full ${health === 'ok' ? 'bg-green-500' : 'bg-red-500'}`} />
            <span className="text-sm text-gray-300">API: {health}</span>
          </div>
        </div>
      </main>
    </div>
  )
}

function App() {
  return (
    <AuthProvider>
      <AppContent />
    </AuthProvider>
  )
}

export default App
