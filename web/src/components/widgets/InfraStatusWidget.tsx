import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { Server, CheckCircle2, AlertTriangle, XCircle } from 'lucide-react'
import { useAuth } from '../../auth'
import Widget from '../Widget'

interface ModuleResult {
  name: string
  status: 'ok' | 'degraded' | 'down' | 'unknown'
  message?: string
}

interface StatusResponse {
  overall: 'ok' | 'degraded' | 'down' | 'unknown'
  modules: ModuleResult[]
}

const statusIcon = {
  ok: <CheckCircle2 size={14} className="text-green-400" />,
  degraded: <AlertTriangle size={14} className="text-yellow-400" />,
  down: <XCircle size={14} className="text-red-400" />,
  unknown: <Server size={14} className="text-gray-500" />,
}

const statusLabel = {
  ok: 'All systems operational',
  degraded: 'Some issues detected',
  down: 'Systems down',
  unknown: 'Status unknown',
}

const overallColor = {
  ok: 'text-green-400',
  degraded: 'text-yellow-400',
  down: 'text-red-400',
  unknown: 'text-gray-500',
}

export default function InfraStatusWidget() {
  const { user } = useAuth()
  const [status, setStatus] = useState<StatusResponse | null>(null)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()

    fetch('/api/infra/status', { credentials: 'include', signal: controller.signal })
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data) setStatus(data)
        setLoaded(true)
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        console.error('InfraStatusWidget fetch error:', err)
        setLoaded(true)
      })

    return () => { controller.abort() }
  }, [user])

  if (!user || !loaded) return null
  // Don't show if no modules are enabled/configured.
  if (loaded && (!status || status.modules.length === 0)) return null

  const issueCount = status!.modules.filter(m => m.status !== 'ok').length

  return (
    <Widget title="Infrastructure">
      <div className="flex items-center gap-2 mb-3">
        <div className={`w-2.5 h-2.5 rounded-full ${
          status!.overall === 'ok' ? 'bg-green-400' :
          status!.overall === 'degraded' ? 'bg-yellow-400' :
          status!.overall === 'down' ? 'bg-red-400' : 'bg-gray-500'
        }`} />
        <span className={`text-sm font-medium ${overallColor[status!.overall]}`}>
          {statusLabel[status!.overall]}
        </span>
      </div>

      {issueCount > 0 && (
        <div className="space-y-1.5 mb-3">
          {status!.modules
            .filter(m => m.status !== 'ok')
            .slice(0, 4)
            .map(m => (
              <div key={m.name} className="flex items-center gap-2 text-xs">
                {statusIcon[m.status]}
                <span className="text-gray-300 truncate">{m.name.replace(/_/g, ' ')}</span>
              </div>
            ))}
        </div>
      )}

      <div className="flex items-center justify-between text-xs text-gray-500">
        <span>{status!.modules.length} modules monitored</span>
        <Link to="/infra" className="text-blue-400 hover:text-blue-300">
          Details →
        </Link>
      </div>
    </Widget>
  )
}
