import { useEffect, useState } from 'react'

function App() {
  const [health, setHealth] = useState<string>('checking...')

  useEffect(() => {
    fetch('/api/health')
      .then(res => res.json())
      .then(data => setHealth(data.status))
      .catch(() => setHealth('offline'))
  }, [])

  return (
    <div className="min-h-screen bg-gray-900 text-white flex items-center justify-center">
      <div className="text-center">
        <h1 className="text-6xl font-bold mb-4">Hytte</h1>
        <p className="text-xl text-gray-400 mb-8">Your cozy corner of the web</p>
        <div className="inline-flex items-center gap-2 bg-gray-800 rounded-full px-4 py-2">
          <span className={`w-2 h-2 rounded-full ${health === 'ok' ? 'bg-green-500' : 'bg-red-500'}`} />
          <span className="text-sm text-gray-300">API: {health}</span>
        </div>
      </div>
    </div>
  )
}

export default App
