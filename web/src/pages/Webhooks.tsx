import { useState, useEffect, useRef } from 'react'
import { useAuth } from '../auth'
import {
  Plus,
  Trash2,
  Copy,
  Check,
  ChevronDown,
  ChevronRight,
  RefreshCw,
  Radio,
  Eraser,
} from 'lucide-react'

interface WebhookEndpoint {
  id: string
  name: string
  created_at: string
}

interface WebhookRequest {
  id: number
  endpoint_id: string
  method: string
  headers: Record<string, string>
  body: string
  query: string
  remote_addr: string
  received_at: string
}

const METHOD_COLORS: Record<string, string> = {
  GET: 'bg-green-600/20 text-green-400',
  POST: 'bg-blue-600/20 text-blue-400',
  PUT: 'bg-yellow-600/20 text-yellow-400',
  PATCH: 'bg-orange-600/20 text-orange-400',
  DELETE: 'bg-red-600/20 text-red-400',
  HEAD: 'bg-purple-600/20 text-purple-400',
  OPTIONS: 'bg-gray-600/20 text-gray-400',
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard write failed — do not flip UI to "copied".
    }
  }

  return (
    <button
      onClick={copy}
      className="text-gray-400 hover:text-white transition-colors cursor-pointer"
      title="Copy to clipboard"
      aria-label="Copy to clipboard"
    >
      {copied ? <Check className="w-4 h-4 text-green-400" /> : <Copy className="w-4 h-4" />}
    </button>
  )
}

function RequestRow({ req }: { req: WebhookRequest }) {
  const [expanded, setExpanded] = useState(false)
  const methodColor = METHOD_COLORS[req.method] || 'bg-gray-600/20 text-gray-400'
  const time = new Date(req.received_at).toLocaleTimeString()

  return (
    <div className="border border-gray-700 rounded-lg overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-3 px-4 py-3 hover:bg-gray-800/50 transition-colors cursor-pointer text-left"
      >
        {expanded ? (
          <ChevronDown className="w-4 h-4 text-gray-400 shrink-0" />
        ) : (
          <ChevronRight className="w-4 h-4 text-gray-400 shrink-0" />
        )}
        <span
          className={`text-xs font-mono font-bold px-2 py-0.5 rounded ${methodColor} shrink-0`}
        >
          {req.method}
        </span>
        <span className="text-sm text-gray-300 truncate">
          {req.query ? `?${req.query}` : '/'}
        </span>
        <span className="text-xs text-gray-500 ml-auto shrink-0">{time}</span>
        <span className="text-xs text-gray-600 shrink-0">{req.remote_addr}</span>
      </button>

      {expanded && (
        <div className="border-t border-gray-700 px-4 py-3 space-y-3 bg-gray-800/30">
          {/* Headers */}
          <div>
            <div className="flex items-center gap-2 mb-1">
              <h4 className="text-xs font-semibold text-gray-400 uppercase">Headers</h4>
              <CopyButton text={JSON.stringify(req.headers, null, 2)} />
            </div>
            <div className="bg-gray-900 rounded p-3 max-h-48 overflow-auto">
              <table className="text-xs font-mono w-full">
                <tbody>
                  {Object.entries(req.headers).map(([k, v]) => (
                    <tr key={k}>
                      <td className="text-blue-400 pr-3 py-0.5 whitespace-nowrap align-top">
                        {k}
                      </td>
                      <td className="text-gray-300 py-0.5 break-all">{v}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {Object.keys(req.headers).length === 0 && (
                <p className="text-gray-500 text-xs">No headers</p>
              )}
            </div>
          </div>

          {/* Body */}
          <div>
            <div className="flex items-center gap-2 mb-1">
              <h4 className="text-xs font-semibold text-gray-400 uppercase">Body</h4>
              {req.body && <CopyButton text={req.body} />}
            </div>
            <pre className="bg-gray-900 rounded p-3 text-xs font-mono text-gray-300 max-h-64 overflow-auto whitespace-pre-wrap break-all">
              {req.body || '(empty)'}
            </pre>
          </div>

          {/* Query String */}
          {req.query && (
            <div>
              <h4 className="text-xs font-semibold text-gray-400 uppercase mb-1">
                Query String
              </h4>
              <pre className="bg-gray-900 rounded p-3 text-xs font-mono text-gray-300">
                {req.query}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default function Webhooks() {
  const { user } = useAuth()
  const [endpoints, setEndpoints] = useState<WebhookEndpoint[]>([])
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const [requests, setRequests] = useState<WebhookRequest[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [live, setLive] = useState(true)
  const eventSourceRef = useRef<EventSource | null>(null)

  const [requestsRefreshKey, setRequestsRefreshKey] = useState(0)

  useEffect(() => {
    if (!user) return
    let cancelled = false
    fetch('/api/webhooks')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (!cancelled && data) setEndpoints(data.endpoints || [])
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [user])

  useEffect(() => {
    if (!selectedID) return
    let cancelled = false
    fetch(`/api/webhooks/${selectedID}/requests`)
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (!cancelled) setRequests(data?.requests || [])
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [selectedID, requestsRefreshKey])

  // SSE subscription for live updates.
  useEffect(() => {
    if (!selectedID || !live) {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
        eventSourceRef.current = null
      }
      return
    }

    const es = new EventSource(`/api/webhooks/${selectedID}/stream`)
    eventSourceRef.current = es

    es.onmessage = (event) => {
      try {
        const req: WebhookRequest = JSON.parse(event.data)
        setRequests((prev) => [req, ...prev].slice(0, 100))
      } catch {
        // Ignore malformed SSE data.
      }
    }

    es.onerror = () => {
      // EventSource will auto-reconnect.
    }

    return () => {
      es.close()
      eventSourceRef.current = null
    }
  }, [selectedID, live])

  const createEndpoint = async () => {
    if (creating) return
    setCreating(true)
    try {
      const res = await fetch('/api/webhooks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName }),
      })
      if (res.ok) {
        const ep = await res.json()
        setEndpoints((prev) => [ep, ...prev])
        setSelectedID(ep.id)
        setNewName('')
      }
    } catch {
      // Network error — leave state as-is.
    } finally {
      setCreating(false)
    }
  }

  const deleteEndpoint = async (id: string) => {
    const res = await fetch(`/api/webhooks/${id}`, { method: 'DELETE' })
    if (res.ok) {
      setEndpoints((prev) => prev.filter((ep) => ep.id !== id))
      if (selectedID === id) {
        setSelectedID(null)
        setRequests([])
      }
    }
  }

  const clearRequests = async () => {
    if (!selectedID) return
    const res = await fetch(`/api/webhooks/${selectedID}/requests`, { method: 'DELETE' })
    if (res.ok) setRequests([])
  }

  const selected = endpoints.find((ep) => ep.id === selectedID)
  const webhookURL = selected
    ? `${window.location.origin}/api/hooks/${selected.id}`
    : null

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <h1 className="text-2xl font-bold mb-6">Webhooks</h1>

      <div className="grid grid-cols-1 lg:grid-cols-[300px_1fr] gap-6">
        {/* Left panel — Endpoint list */}
        <div>
          <div className="flex items-center gap-2 mb-3">
            <input
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="Endpoint name"
              className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white flex-1 focus:outline-none focus:ring-2 focus:ring-blue-500"
              onKeyDown={(e) => e.key === 'Enter' && !creating && createEndpoint()}
            />
            <button
              onClick={createEndpoint}
              disabled={creating}
              className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white p-2 rounded-lg transition-colors cursor-pointer"
              title="Create endpoint"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>

          {loading ? (
            <p className="text-gray-400 text-sm">Loading endpoints...</p>
          ) : endpoints.length === 0 ? (
            <p className="text-gray-500 text-sm">
              No endpoints yet. Create one to get started — it's a reel catch for debugging!
            </p>
          ) : (
            <div className="space-y-1">
              {endpoints.map((ep) => (
                <div
                  key={ep.id}
                  className={`flex items-center gap-2 rounded-lg px-3 py-2 cursor-pointer transition-colors group ${
                    selectedID === ep.id
                      ? 'bg-blue-600/20 border border-blue-500/30'
                      : 'hover:bg-gray-800 border border-transparent'
                  }`}
                  onClick={() => setSelectedID(ep.id)}
                >
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium truncate">{ep.name}</p>
                    <p className="text-xs text-gray-500 font-mono truncate">{ep.id}</p>
                  </div>
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      deleteEndpoint(ep.id)
                    }}
                    className="text-gray-600 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-all cursor-pointer"
                    title="Delete endpoint"
                    aria-label={`Delete endpoint ${ep.name}`}
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Right panel — Request inspector */}
        <div>
          {!selected ? (
            <div className="flex items-center justify-center h-64 text-gray-500 text-sm">
              Select an endpoint to inspect incoming requests
            </div>
          ) : (
            <>
              {/* Endpoint URL bar */}
              <div className="bg-gray-800 rounded-lg p-4 mb-4">
                <div className="flex items-center gap-2 mb-2">
                  <h2 className="text-lg font-semibold">{selected.name}</h2>
                  {live && (
                    <span className="flex items-center gap-1 text-xs text-green-400">
                      <Radio className="w-3 h-3 animate-pulse" />
                      Live
                    </span>
                  )}
                </div>
                <div className="flex items-center gap-2">
                  <code className="bg-gray-900 text-sm text-gray-300 px-3 py-1.5 rounded flex-1 truncate font-mono">
                    {webhookURL}
                  </code>
                  <CopyButton text={webhookURL!} />
                </div>
                <p className="text-xs text-gray-500 mt-2">
                  Send any HTTP request to this URL. All methods accepted.
                </p>
              </div>

              {/* Controls */}
              <div className="flex items-center gap-2 mb-4">
                <button
                  onClick={() => setLive(!live)}
                  className={`flex items-center gap-1.5 text-sm px-3 py-1.5 rounded-lg transition-colors cursor-pointer ${
                    live
                      ? 'bg-green-600/20 text-green-400 hover:bg-green-600/30'
                      : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
                  }`}
                >
                  <Radio className="w-3.5 h-3.5" />
                  {live ? 'Live' : 'Paused'}
                </button>
                <button
                  onClick={() => setRequestsRefreshKey((k) => k + 1)}
                  className="flex items-center gap-1.5 text-sm px-3 py-1.5 rounded-lg bg-gray-700 text-gray-400 hover:bg-gray-600 transition-colors cursor-pointer"
                  title="Refresh"
                >
                  <RefreshCw className="w-3.5 h-3.5" />
                  Refresh
                </button>
                <button
                  onClick={clearRequests}
                  className="flex items-center gap-1.5 text-sm px-3 py-1.5 rounded-lg bg-gray-700 text-gray-400 hover:bg-gray-600 transition-colors cursor-pointer"
                  title="Clear all requests"
                >
                  <Eraser className="w-3.5 h-3.5" />
                  Clear
                </button>
                <span className="text-xs text-gray-500 ml-auto">
                  {requests.length} request{requests.length !== 1 ? 's' : ''}
                </span>
              </div>

              {/* Request list */}
              {requests.length === 0 ? (
                <div className="flex flex-col items-center justify-center h-48 text-gray-500 text-sm">
                  <p>No requests received yet.</p>
                  <p className="text-xs mt-1">
                    Try: <code className="bg-gray-800 px-1 rounded">curl {webhookURL}</code>
                  </p>
                </div>
              ) : (
                <div className="space-y-2">
                  {requests.map((req) => (
                    <RequestRow key={req.id} req={req} />
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}
