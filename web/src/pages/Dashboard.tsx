import { useAuth } from '../auth'

function Dashboard() {
  const { user } = useAuth()

  if (!user) return null

  const memberSince = new Date(user.created_at).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })

  return (
    <main className="flex items-center justify-center" style={{ minHeight: 'calc(100vh - 72px)' }}>
      <div className="w-full max-w-md bg-gray-800 rounded-2xl p-8 text-center">
        <img
          src={user.picture}
          alt={user.name}
          className="w-24 h-24 rounded-full mx-auto mb-4 border-2 border-gray-600"
          referrerPolicy="no-referrer"
        />
        <h1 className="text-2xl font-bold mb-1">Welcome, {user.name.split(' ')[0]}!</h1>
        <p className="text-gray-400 mb-6">Glad to have you at the Hytte.</p>

        <dl className="text-left space-y-4">
          <div>
            <dt className="text-xs uppercase tracking-wide text-gray-500">Name</dt>
            <dd className="text-lg">{user.name}</dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-gray-500">Email</dt>
            <dd className="text-lg">{user.email}</dd>
          </div>
          <div>
            <dt className="text-xs uppercase tracking-wide text-gray-500">Member since</dt>
            <dd className="text-lg">{memberSince}</dd>
          </div>
        </dl>
      </div>
    </main>
  )
}

export default Dashboard
