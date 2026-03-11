import { useState, useEffect, useRef, useMemo } from 'react'
import type { ComponentType } from 'react'
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
  Terminal,
  ExternalLink,
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

// ── Webhook source detection ────────────────────────────────────────────────

type WebhookSource = 'github' | 'slack' | 'stripe' | 'forge' | 'generic'

const SOURCE_STYLES: Record<WebhookSource, { label: string; cls: string }> = {
  github: { label: 'GH', cls: 'bg-gray-700 text-gray-200' },
  slack: { label: 'SL', cls: 'bg-purple-900/60 text-purple-300' },
  stripe: { label: 'ST', cls: 'bg-indigo-900/60 text-indigo-300' },
  forge: { label: 'FG', cls: 'bg-orange-900/60 text-orange-300' },
  generic: { label: 'WH', cls: 'bg-gray-700/50 text-gray-500' },
}

interface ParsedWebhook {
  source: WebhookSource
  summary: string
  details: string[]
  parsedBody: unknown | null
}

function tryParseJSON(str: string): unknown | null {
  if (!str) return null
  try {
    return JSON.parse(str)
  } catch {
    return null
  }
}

function isObject(val: unknown): val is Record<string, unknown> {
  return typeof val === 'object' && val !== null && !Array.isArray(val)
}

function humanizeEvent(event: string): string {
  return event.replace(/_/g, ' ').replace(/^\w/, (c) => c.toUpperCase())
}

function detectSource(headers: Record<string, string>): {
  source: WebhookSource
  lower: Record<string, string>
} {
  const lower = Object.fromEntries(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v]))
  if (lower['x-github-event'] !== undefined) return { source: 'github', lower }
  if (lower['stripe-signature'] !== undefined) return { source: 'stripe', lower }
  const ua = (lower['user-agent'] || '').toLowerCase()
  if (ua.includes('slackbot') || ua.includes('slack')) return { source: 'slack', lower }
  if (ua.includes('go-http-client')) return { source: 'forge', lower }
  return { source: 'generic', lower }
}

function parseGitHubSummary(
  eventType: string,
  body: Record<string, unknown>,
): { summary: string; details: string[] } {
  const repo = (body.repository as Record<string, unknown>)?.full_name as string | undefined
  const sender = (body.sender as Record<string, unknown>)?.login as string | undefined
  const details: string[] = []
  if (repo) details.push(`repo: ${repo}`)
  if (sender) details.push(`by: ${sender}`)

  switch (eventType) {
    case 'push': {
      const ref = body.ref as string | undefined
      const commits = body.commits as unknown[] | undefined
      const branch = ref?.replace('refs/heads/', '') ?? 'unknown'
      const count = commits?.length ?? 0
      return {
        summary: `Push to ${branch}: ${count} commit${count !== 1 ? 's' : ''}`,
        details,
      }
    }
    case 'pull_request': {
      const pr = body.pull_request as Record<string, unknown> | undefined
      const action = body.action as string | undefined
      const num = pr?.number
      const title = pr?.title as string | undefined
      return {
        summary: `PR ${action ?? 'updated'}${num != null ? ` #${num}` : ''}${title ? `: ${title}` : ''}`,
        details,
      }
    }
    case 'release': {
      const release = body.release as Record<string, unknown> | undefined
      const action = body.action as string | undefined
      const tag = release?.tag_name as string | undefined
      return {
        summary: `Release ${action ?? 'published'}${tag ? `: ${tag}` : ''}`,
        details,
      }
    }
    case 'issues': {
      const issue = body.issue as Record<string, unknown> | undefined
      const action = body.action as string | undefined
      const num = issue?.number
      const title = issue?.title as string | undefined
      return {
        summary: `Issue ${action ?? 'updated'}${num != null ? ` #${num}` : ''}${title ? `: ${title}` : ''}`,
        details,
      }
    }
    case 'issue_comment': {
      const issue = body.issue as Record<string, unknown> | undefined
      return { summary: `Comment on issue${issue?.number != null ? ` #${issue.number}` : ''}`, details }
    }
    case 'workflow_run': {
      const wf = body.workflow_run as Record<string, unknown> | undefined
      const name = wf?.name as string | undefined
      const conclusion = wf?.conclusion as string | undefined
      return {
        summary: `Workflow ${name ?? ''}${conclusion ? ` — ${conclusion}` : ''}`.trim(),
        details,
      }
    }
    case 'ping':
      return { summary: repo ? `Ping from ${repo}` : 'GitHub ping', details }
    case 'create':
    case 'delete': {
      const refType = body.ref_type as string | undefined
      const ref = body.ref as string | undefined
      return {
        summary: `${eventType === 'create' ? 'Created' : 'Deleted'} ${refType ?? 'ref'}${ref ? ` ${ref}` : ''}`,
        details,
      }
    }
    default:
      return { summary: `GitHub ${eventType} event`, details }
  }
}

function parseWebhook(headers: Record<string, string>, body: string): ParsedWebhook {
  const { source, lower } = detectSource(headers)
  const parsedBody = tryParseJSON(body)
  const parsed = isObject(parsedBody) ? parsedBody : null

  if (source === 'github' && parsed) {
    const eventType = lower['x-github-event'] || 'unknown'
    const { summary, details } = parseGitHubSummary(eventType, parsed)
    return { source, summary, details, parsedBody }
  }

  if (source === 'stripe' && parsed) {
    const eventType = parsed.type as string | undefined
    const obj = (parsed.data as Record<string, unknown>)?.object as
      | Record<string, unknown>
      | undefined
    const objType = obj?.object as string | undefined
    return {
      source,
      summary: eventType ? `Stripe ${eventType}${objType ? ` (${objType})` : ''}` : 'Stripe event',
      details: [],
      parsedBody,
    }
  }

  if (source === 'slack' && parsed) {
    const channel = parsed.channel_name as string | undefined
    const user = parsed.user_name as string | undefined
    const text = parsed.text as string | undefined
    const parts = [
      channel && `#${channel}`,
      user && `@${user}`,
      text && `"${text.slice(0, 60)}${text.length > 60 ? '…' : ''}"`,
    ].filter(Boolean)
    return { source, summary: parts.join(' ') || 'Slack event', details: [], parsedBody }
  }

  if (source === 'forge' && parsed) {
    const str = (v: unknown): string | undefined => (typeof v === 'string' ? v : undefined)
    const event = str(parsed.event)
    const version = str(parsed.version) || str(parsed.tag)
    const detail = str(parsed.changelog_summary) || str(parsed.description)
    const releaseUrl = str(parsed.release_url)
    let project: string | undefined
    if (releaseUrl) {
      try {
        project = new URL(releaseUrl).hostname.split('.')[0]
      } catch {
        /* ignore */
      }
    }
    if (event) {
      const parts = [humanizeEvent(event), version].filter(Boolean).join(': ')
      const summary = project ? `${parts} (${project})` : parts
      return { source: 'forge', summary: summary || 'Forge event', details: detail ? [detail] : [], parsedBody }
    }
    // Fall through to generic if no event field
  }

  // Generic: look for common event/action/type fields with enriched context
  if (parsed) {
    const str = (v: unknown): string | undefined => (typeof v === 'string' ? v : undefined)
    const rawEvent =
      str(parsed.event) ||
      str(parsed.action) ||
      str(parsed.type) ||
      str(parsed.event_type)
    const event = rawEvent ? humanizeEvent(rawEvent) : undefined
    const version = str(parsed.version) || str(parsed.tag) || str(parsed.tag_name)
    const name =
      str(parsed.title) ||
      str(parsed.subject) ||
      str(parsed.name) ||
      str((parsed.repository as Record<string, unknown>)?.name)
    let urlProject: string | undefined
    for (const key of Object.keys(parsed)) {
      if (key.endsWith('_url') && typeof parsed[key] === 'string') {
        try {
          urlProject = new URL(parsed[key] as string).hostname.split('.')[0]
          break
        } catch {
          /* skip */
        }
      }
    }
    const detail =
      str(parsed.changelog_summary) ||
      str(parsed.description) ||
      str(parsed.message) ||
      str(parsed.text)
    const mainParts = [event, version || name].filter(Boolean).join(': ')
    const context = !version && !name && urlProject ? ` (${urlProject})` : ''
    const summary = mainParts ? `${mainParts}${context}` : undefined
    if (summary) return { source: 'generic', summary, details: detail ? [detail] : [], parsedBody }
  }

  // Fallback: byte count
  const bytes = new TextEncoder().encode(body).length
  return {
    source: 'generic',
    summary: bytes > 0 ? `${bytes} bytes` : 'empty body',
    details: [],
    parsedBody,
  }
}

function formatRelativeTime(date: Date): string {
  const diff = Date.now() - date.getTime()
  if (diff < 60_000) return 'just now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} min ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} hr ago`
  return date.toLocaleDateString(undefined)
}

function extractURLs(body: string): string[] {
  const pattern = /https?:\/\/[^\s"'<>{}|\\^[\]`]+/g
  return [...new Set(body.match(pattern) ?? [])]
}

// Escape a string for safe inclusion inside single quotes in POSIX shell.
// Replaces each ' with '\'' (close quote, literal quote, reopen quote).
function shellEscapeSingleQuoted(value: string): string {
  return value.replace(/'/g, "'\\''")
}

function buildCurlCommand(req: WebhookRequest, endpointURL: string): string {
  const rawUrl = req.query ? `${endpointURL}?${req.query}` : endpointURL
  const url = shellEscapeSingleQuoted(rawUrl)
  const parts = [`curl -X ${req.method} '${url}'`]
  const contentType = Object.entries(req.headers).find(
    ([k]) => k.toLowerCase() === 'content-type',
  )
  if (contentType) {
    const headerName = shellEscapeSingleQuoted(contentType[0])
    const headerValue = shellEscapeSingleQuoted(contentType[1])
    parts.push(`  -H '${headerName}: ${headerValue}'`)
  }
  if (req.body) parts.push(`  -d '${shellEscapeSingleQuoted(req.body)}'`)
  return parts.join(' \\\n')
}

// ── JSON syntax highlighter ─────────────────────────────────────────────────

function JsonHighlight({ value }: { value: string }) {
  const tokens: { text: string; cls: string }[] = []
  let lastIndex = 0
  // Matches quoted strings (keys end with :), booleans, null, numbers
  const re =
    /("(?:\\u[0-9a-fA-F]{4}|\\[^u]|[^\\"])*"(?:\s*:)?|true|false|null|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g
  let m: RegExpExecArray | null
  while ((m = re.exec(value)) !== null) {
    if (m.index > lastIndex) {
      tokens.push({ text: value.slice(lastIndex, m.index), cls: 'text-gray-400' })
    }
    const token = m[0]
    let cls: string
    if (token.startsWith('"')) {
      cls = token.endsWith(':') ? 'text-blue-400' : 'text-green-400'
    } else if (token === 'true' || token === 'false') {
      cls = 'text-orange-400'
    } else if (token === 'null') {
      cls = 'text-red-400'
    } else {
      cls = 'text-yellow-400'
    }
    tokens.push({ text: token, cls })
    lastIndex = re.lastIndex
  }
  if (lastIndex < value.length) {
    tokens.push({ text: value.slice(lastIndex), cls: 'text-gray-400' })
  }

  return (
    <pre className="bg-gray-900 rounded p-3 text-xs font-mono max-h-96 overflow-auto whitespace-pre-wrap break-words leading-relaxed">
      {tokens.map((t, i) => (
        <span key={i} className={t.cls}>
          {t.text}
        </span>
      ))}
    </pre>
  )
}

// ── Shared components ────────────────────────────────────────────────────────

function CopyButton({
  text,
  icon: Icon = Copy,
  title: titleProp = 'Copy to clipboard',
}: {
  text: string
  icon?: ComponentType<{ className?: string }>
  title?: string
}) {
  const [copied, setCopied] = useState(false)
  const timeoutRef = useRef<number | null>(null)

  useEffect(() => {
    return () => {
      if (timeoutRef.current !== null) {
        clearTimeout(timeoutRef.current)
        timeoutRef.current = null
      }
    }
  }, [])

  const copy = async () => {
    try {
      if (timeoutRef.current !== null) {
        clearTimeout(timeoutRef.current)
        timeoutRef.current = null
      }
      await navigator.clipboard.writeText(text)
      setCopied(true)
      timeoutRef.current = window.setTimeout(() => {
        setCopied(false)
        timeoutRef.current = null
      }, 2000)
    } catch {
      // Clipboard write failed — do not flip UI to "copied".
    }
  }

  return (
    <button
      onClick={copy}
      className="text-gray-400 hover:text-white transition-colors cursor-pointer"
      title={titleProp}
      aria-label={titleProp}
    >
      {copied ? <Check className="w-4 h-4 text-green-400" /> : <Icon className="w-4 h-4" />}
    </button>
  )
}

function SourceBadge({ source }: { source: WebhookSource }) {
  const { label, cls } = SOURCE_STYLES[source]
  return (
    <span className={`text-xs font-bold px-1.5 py-0.5 rounded shrink-0 ${cls}`}>{label}</span>
  )
}

// ── Request row ──────────────────────────────────────────────────────────────

function RequestRow({ req, endpointURL }: { req: WebhookRequest; endpointURL: string }) {
  const [expanded, setExpanded] = useState(false)
  const [headersExpanded, setHeadersExpanded] = useState(false)

  const methodColor = METHOD_COLORS[req.method] || 'bg-gray-600/20 text-gray-400'
  const parsed = useMemo(
    () => parseWebhook(req.headers, req.body),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [req.id, req.body, req.headers],
  )
  const receivedAt = new Date(req.received_at)
  const relTime = formatRelativeTime(receivedAt)
  const fullTime = receivedAt.toLocaleString(undefined)

  const parsedJSON = parsed.parsedBody
  const prettyBody = useMemo(
    () => (parsedJSON !== null ? JSON.stringify(parsedJSON, null, 2) : req.body),
    [parsedJSON, req.body],
  )
  const isJSON = parsedJSON !== null
  const urls = extractURLs(req.body)
  const curlCmd = buildCurlCommand(req, endpointURL)
  const headerCount = Object.keys(req.headers).length

  return (
    <div className="border border-gray-700 rounded-lg overflow-hidden">
      {/* Summary row */}
      <button
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
        className="w-full flex items-center gap-2 px-4 py-3 hover:bg-gray-800/50 transition-colors cursor-pointer text-left"
      >
        {expanded ? (
          <ChevronDown className="w-4 h-4 text-gray-400 shrink-0" />
        ) : (
          <ChevronRight className="w-4 h-4 text-gray-400 shrink-0" />
        )}
        <SourceBadge source={parsed.source} />
        <span className={`text-xs font-mono font-bold px-2 py-0.5 rounded ${methodColor} shrink-0`}>
          {req.method}
        </span>
        <span className="text-sm text-gray-300 truncate flex-1">{parsed.summary}</span>
        <span
          className="text-xs text-gray-500 shrink-0 tabular-nums"
          title={fullTime}
        >
          {relTime}
        </span>
      </button>

      {expanded && (
        <div className="border-t border-gray-700 px-4 py-3 space-y-3 bg-gray-800/30">
          {/* Source details card */}
          {parsed.details.length > 0 && (
            <div className="bg-gray-900/60 rounded px-3 py-2 flex flex-wrap gap-x-4 gap-y-1">
              <span className="text-xs text-gray-500 font-semibold uppercase">{parsed.source}</span>
              {parsed.details.map((d) => (
                <span key={d} className="text-xs text-gray-400 font-mono">
                  {d}
                </span>
              ))}
            </div>
          )}

          {/* Body — most prominent, shown first */}
          <div>
            <div className="flex items-center gap-2 mb-1">
              <h4 className="text-xs font-semibold text-gray-400 uppercase">Body</h4>
              {req.body && (
                <>
                  <CopyButton
                    text={prettyBody}
                    title={isJSON ? 'Copy formatted JSON' : 'Copy body'}
                  />
                  <CopyButton text={curlCmd} icon={Terminal} title="Copy curl command" />
                </>
              )}
            </div>
            {req.body ? (
              isJSON ? (
                <JsonHighlight value={prettyBody} />
              ) : (
                <pre className="bg-gray-900 rounded p-3 text-xs font-mono text-gray-300 max-h-64 overflow-auto whitespace-pre-wrap break-words">
                  {req.body}
                </pre>
              )
            ) : (
              <p className="text-gray-500 text-xs italic">(empty)</p>
            )}

            {/* Clickable URLs found in body */}
            {urls.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-2">
                {urls.map((url) => (
                  <a
                    key={url}
                    href={url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300 truncate max-w-xs"
                    title={url}
                  >
                    <ExternalLink className="w-3 h-3 shrink-0" />
                    <span className="truncate">{url}</span>
                  </a>
                ))}
              </div>
            )}
          </div>

          {/* Headers — collapsed by default */}
          <div>
            <div className="flex items-center gap-1.5">
              <button
                onClick={() => setHeadersExpanded(!headersExpanded)}
                className="flex items-center gap-1.5 text-xs font-semibold text-gray-500 uppercase hover:text-gray-300 transition-colors cursor-pointer"
                aria-expanded={headersExpanded}
              >
                {headersExpanded ? (
                  <ChevronDown className="w-3.5 h-3.5" />
                ) : (
                  <ChevronRight className="w-3.5 h-3.5" />
                )}
                Headers ({headerCount})
              </button>
              {headersExpanded && (
                <CopyButton text={JSON.stringify(req.headers, null, 2)} />
              )}
            </div>
            {headersExpanded && (
              <div className="bg-gray-900 rounded p-3 mt-1 max-h-48 overflow-auto">
                <table className="text-xs font-mono w-full">
                  <tbody>
                    {Object.entries(req.headers)
                      .sort(([a], [b]) => a.localeCompare(b))
                      .map(([k, v]) => (
                        <tr key={k}>
                          <td className="text-blue-400 pr-3 py-0.5 whitespace-nowrap align-top">
                            {k}
                          </td>
                          <td className="text-gray-300 py-0.5 break-all">{v}</td>
                        </tr>
                      ))}
                  </tbody>
                </table>
                {headerCount === 0 && <p className="text-gray-500 text-xs">No headers</p>}
              </div>
            )}
          </div>

          {/* Query String */}
          {req.query && (
            <div>
              <h4 className="text-xs font-semibold text-gray-400 uppercase mb-1">Query String</h4>
              <pre className="bg-gray-900 rounded p-3 text-xs font-mono text-gray-300">
                {req.query}
              </pre>
            </div>
          )}

          {/* Remote address */}
          <p className="text-xs text-gray-600">from {req.remote_addr}</p>
        </div>
      )}
    </div>
  )
}

// ── Main page ────────────────────────────────────────────────────────────────

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
    Promise.resolve()
      .then(() => {
        if (!cancelled) setRequests([])
        return fetch(`/api/webhooks/${selectedID}/requests`)
      })
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
    try {
      const res = await fetch(`/api/webhooks/${id}`, { method: 'DELETE' })
      if (res.ok) {
        setEndpoints((prev) => prev.filter((ep) => ep.id !== id))
        if (selectedID === id) {
          setSelectedID(null)
          setRequests([])
        }
      }
    } catch {
      // Network error — leave state as-is.
    }
  }

  const clearRequests = async () => {
    if (!selectedID) return
    try {
      const res = await fetch(`/api/webhooks/${selectedID}/requests`, { method: 'DELETE' })
      if (res.ok) setRequests([])
    } catch {
      // Network error — leave requests as-is.
    }
  }

  const selected = endpoints.find((ep) => ep.id === selectedID)
  const webhookURL = selected ? `${window.location.origin}/api/hooks/${selected.id}` : null

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
              aria-label="Create endpoint"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>

          {loading ? (
            <p className="text-gray-400 text-sm">Loading endpoints...</p>
          ) : endpoints.length === 0 ? (
            <p className="text-gray-500 text-sm">
              No endpoints yet. Create one to get started — it&apos;s a reel catch for debugging!
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
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      setSelectedID(ep.id)
                    }
                  }}
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
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.stopPropagation()
                      }
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
                    <RequestRow key={req.id} req={req} endpointURL={webhookURL!} />
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
