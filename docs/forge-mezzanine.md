# Forge Mezzanine — Mission Control for the Forge

**Status**: Planning
**Date**: 2026-04-04
**Supersedes**: forge-web-dashboard.md (the basic dashboard plan)

---

## Overview

The **Mezzanine** is Forge's web command center — the elevated walkway where the forgemaster surveys all operations below. Not a TUI port. A purpose-built web experience that shows *more*, does *more*, and looks *better* than Hearth ever could.

The current Hearth TUI crams everything into three text columns. The Mezzanine uses the full power of the browser: resizable panels, live streaming output, interactive charts, drag-to-reorder, deep links, push notifications, and a spatial layout that lets you see the *flow* of work, not just lists of items.

## Design Principles

1. **Glanceable** — Status of the entire forge in under 2 seconds
2. **Actionable** — Every item has context actions (merge, retry, kill) within reach
3. **Streamable** — Live worker output, not just "running for 2m 14s"
4. **Mobile-first** — Check forge status from your phone at the dinner table
5. **Information-dense** — More data per pixel than Hearth, but never cluttered

## Architecture

Same as the existing dashboard plan — Hytte reads Forge's state.db (read-only) and sends commands via IPC socket. Both run on the same Hetzner box.

```
Browser  ──►  Hytte (Go + React)  ──►  Forge Daemon
                │                         │
                │  Reads: state.db        │  state.db (WAL)
                │  Commands: IPC socket   │  IPC socket
                │  Events: SSE stream     │  Broadcast events
```

## The Layout

### Main View: `/forge/mezzanine`

A mission-control grid with five zones. Responsive: on desktop all visible, on mobile they stack as expandable cards.

```
┌─────────────────────────────────────────────────────────────┐
│  ⬡ Mezzanine            ● Running  3/6 smiths  $4.23 today │  ← Status Bar
├────────────────────┬────────────────────────────────────────┤
│                    │                                        │
│   QUEUE            │         FORGE FLOOR                    │
│                    │                                        │
│   ┌──────────┐     │   ┌─────────────────────────────────┐  │
│   │ Hytte-ab │←drag│   │  Worker 1: Hytte-w8fd           │  │
│   │ Hytte-cd │     │   │  Phase: smith  ⏱ 2m 14s         │  │
│   │ Forge-ef │     │   │  ┌─────────────────────────┐    │  │
│   │ Forge-gh │     │   │  │ Analyzing codebase...   │    │  │
│   └──────────┘     │   │  │ Reading src/auth/...    │    │  │
│                    │   │  │ Creating handler...     │◄── live stream
│   Needs Attention  │   │  └─────────────────────────┘    │  │
│   ┌──────────┐     │   ├─────────────────────────────────┤  │
│   │ ⊘ abc [R]│     │   │  Worker 2: Forge-t6y9           │  │
│   │ ⊘ def [R]│     │   │  Phase: temper  ⏱ 45s           │  │
│   └──────────┘     │   │  Running: go test ./...          │  │
│                    │   ├─────────────────────────────────┤  │
│                    │   │  Worker 3: (idle)                │  │
│                    │   └─────────────────────────────────┘  │
├────────────────────┴────────────────────────────────────────┤
│                                                             │
│  PIPELINE        Queue → Smith → Temper → Warden → PR      │
│                                                             │
│  PR #486 ████████████████████░░░░  CI passing, 1 approval   │
│  PR #485 ████████████████████████  Ready to merge  [Merge]  │
│  PR #484 ████████░░░░░░░░░░░░░░░  Warden reviewing...      │
│                                                             │
├──────────────────────────────┬──────────────────────────────┤
│  EVENTS                      │  COSTS                       │
│                              │                              │
│  08:12 pr_merged #486        │  ┌─ Today ────────────────┐  │
│  08:08 smith_done w8fd $0.42 │  │  $4.23 / $25.00 limit │  │
│  08:05 warden_pass t6y9      │  │  ████████░░░░  17%     │  │
│  08:00 bead_claimed w8fd     │  └────────────────────────┘  │
│  07:55 temper_pass abc       │                              │
│  07:50 poll 3 ready          │  ┌─ This Week ───────────┐  │
│                              │  │  📈 sparkline chart    │  │
│  [Filter ▾] [All events →]  │  │  $32.10 total          │  │
│                              │  └────────────────────────┘  │
└──────────────────────────────┴──────────────────────────────┘
```

### Zone 1: Status Bar (top)

Always visible. Shows at a glance:
- Daemon status indicator (green dot = running)
- Active workers / total slots (e.g., "3/6 smiths")
- Today's cost with daily limit indicator
- Copilot premium request usage (if applicable)
- Last poll timestamp
- Quick actions: Refresh, Rebuild & Restart

### Zone 2: Queue (left sidebar)

The queue of ready beads, grouped by anvil. Each bead shows:
- Bead ID + title (truncated)
- Priority badge
- Anvil indicator (color-coded)
- Age (how long it's been waiting)

**Interactions:**
- **Drag to reorder** priority within an anvil
- **Click** to expand: full title, description, dependencies
- **Right-click / long-press** context menu: Run Now, Clarify, Tag, Dismiss
- **"Needs Attention" section** below queue: beads that failed, stalled, or need clarification. Each with Retry / Clarify / Dismiss buttons.

### Zone 3: Forge Floor (center, the main stage)

The beating heart of the Mezzanine. Shows active workers with **live streaming output**.

Each worker panel shows:
- Bead ID, anvil, title
- Current phase (smith / temper / warden / bellows) with phase icon
- Duration timer
- Provider badge (Claude / Gemini / Copilot)
- **Live output stream** — the actual Claude/tool output, syntax-highlighted, auto-scrolling
- Action buttons: Kill, Force Iteration, View Full Log
- Cost accumulator for this bead

**Live streaming implementation — REUSE EXISTING COMPONENTS:**

The current dashboard already has excellent live activity streaming. Reuse:

- **`LiveActivity.tsx`** (523 lines) — SSE streaming from `/api/forge/activity/stream`,
  structured log parsing (tool_use/text/think), Markdown rendering via ReactMarkdown,
  auto-scroll with scroll lock, color-coded log levels, collapsible tool use sections
  with success/error status indicators. Already production-tested.
- **`WorkerLogModal.tsx`** (176 lines) — Full log viewer modal for completed workers,
  fetches from `/api/forge/workers/{id}/log/parsed`.
- **`useForgeStatus` hook** — existing hook for polling forge status/workers/events.
- **SSE endpoint** `/api/forge/activity/stream` — already implemented in the Go backend.

For the Mezzanine, adapt `LiveActivity` to work inside the worker panels:
- Each worker panel embeds a `LiveActivity` instance filtered to that worker
- The full-width "Forge Floor" can show the selected worker's stream expanded
- Click a worker panel to focus/expand its stream
- Collapsed worker panels show last 2-3 lines as a preview

**Idle worker slots** shown as empty panels with a subtle "waiting for work" animation.

### Zone 4: Pipeline (middle bar)

A horizontal progress visualization showing beads flowing through the pipeline:

```
Queue → Schematic → Smith → Temper → Warden → PR → Merged
```

Each active bead is shown as a card at its current stage. PRs show:
- PR number and title
- CI status (green check / red X / yellow spinner)
- Review status (approved / changes requested / pending)
- Merge readiness (ready to merge = green glow + Merge button)
- Bellows status (fix attempts, rebase needed)

**Interactions:**
- Click PR card to expand: full details, link to GitHub, CI check list
- **Merge button** on ready PRs (with confirmation dialog)
- **Quench / Burnish / Rebase** actions on PRs with issues

### Zone 5: Events + Costs (bottom, split)

**Events panel (left):**
- Reverse-chronological event log
- Color-coded by type (green for success, red for failures, blue for info)
- Filter dropdown: All, Errors only, PRs only, per-anvil
- Click event to see related bead/PR details
- "Show all events" link to full events page

**Costs panel (right):**
- Today's spend vs daily limit (progress bar)
- Sparkline chart of last 7 days
- Per-anvil cost breakdown (mini bar chart)
- Click through to full cost dashboard page

## Detail Pages

### `/forge/mezzanine/worker/{id}` — Worker Detail

Full-screen view of a single worker:
- Complete log output (not just tail)
- Phase timeline: when did smith start, how long was temper, etc.
- Token usage breakdown (input/output/cache)
- Cost for this bead
- Related events
- Link to PR if created

### `/forge/mezzanine/events` — Full Event Log

- Paginated, filterable, searchable
- Date range picker
- Export to CSV
- Event correlation: click an event to see all events for that bead's lifecycle

### `/forge/mezzanine/costs` — Cost Dashboard

- Daily cost chart (Recharts area chart, last 30 days)
- Weekly cost chart
- Per-anvil breakdown (stacked bar chart)
- Top 10 most expensive beads this month (table)
- Cost per bead distribution (histogram)
- Budget alert configuration

### `/forge/mezzanine/ingots` — Bead Lifecycle Tracking

- Full bead lifecycle view: created → claimed → smith → temper → warden → PR → merged
- Filter by anvil, status, date range
- Duration metrics: how long does each phase take on average?
- Success rate: what % of beads make it to merge on first try?

### `/forge/mezzanine/anvils` — Anvil Health

- Per-anvil overview: last activity, open PRs, failing CI, queue depth
- Health indicators (green/yellow/red)
- Click through to anvil-specific filtered views

## Real-Time Updates

**Primary: Server-Sent Events (SSE)**

Hytte subscribes to Forge's IPC event broadcast and forwards to the browser:

```
GET /api/forge/events/stream
Content-Type: text/event-stream

data: {"type":"smith_done","bead":"Hytte-w8fd","cost":0.42}
data: {"type":"pr_created","bead":"Hytte-w8fd","pr":487}
data: {"type":"bead_claimed","bead":"Forge-t6y9"}
```

The frontend dispatches events to update the relevant zone:
- Worker events → Forge Floor
- PR events → Pipeline
- Cost events → Costs panel
- All events → Events panel

**Worker log streaming:**
Separate SSE endpoint per worker. Hytte tails the log file and streams chunks:

```
GET /api/forge/worker/{id}/stream
Content-Type: text/event-stream

data: {"line":"Analyzing codebase...","ts":"2026-04-04T08:12:00Z"}
data: {"line":"Reading src/auth/handlers.go","ts":"2026-04-04T08:12:01Z"}
```

**Fallback: Polling**
If SSE connection drops, fall back to polling every 5 seconds. The UI should handle this transparently (show a "reconnecting..." indicator).

## Mobile Layout

On screens < 768px, the mission control grid collapses to stacked cards:

1. **Status bar** (sticky top)
2. **Forge Floor** (workers, collapsed to summary unless expanded)
3. **Pipeline** (horizontal scroll for PR cards)
4. **Queue** (collapsible)
5. **Needs Attention** (always visible if items exist — this is why you check your phone)
6. **Events** (last 5, expandable)
7. **Costs** (today's summary only)

Big touch targets on action buttons (44px minimum). Swipe gestures for common actions (swipe PR to merge, swipe bead to retry).

## Push Notification Deep Links

When Forge sends a webhook notification:
- "PR ready to merge" → links to `/forge/mezzanine` with the PR highlighted
- "Bead failed" → links to `/forge/mezzanine` with the needs-attention item expanded
- "Cost limit reached" → links to `/forge/mezzanine/costs`
- "Worker stalled" → links to the worker detail page

## Keyboard Shortcuts (Desktop)

| Key | Action |
|-----|--------|
| `r` | Refresh (trigger poll) |
| `m` | Merge first ready PR |
| `k` | Kill focused worker |
| `q` | Focus queue panel |
| `w` | Focus workers panel |
| `e` | Focus events panel |
| `1-6` | Focus worker 1-6 |
| `?` | Show shortcut help |

## Existing Components to Reuse

The current dashboard (ForgeDashboardPage.tsx) already has significant infrastructure:

| Component | Lines | What it does | Reuse in Mezzanine |
|-----------|-------|-------------|-------------------|
| `LiveActivity.tsx` | 523 | SSE log streaming, Markdown, tool_use sections, auto-scroll | Worker panels on Forge Floor |
| `WorkerLogModal.tsx` | 176 | Full log viewer for completed workers | Worker detail page |
| `useForgeStatus.ts` | hook | Polls `/api/forge/status`, workers, events | Status bar, all zones |
| `useAllPRs.ts` | hook | Fetches PR data across anvils | Pipeline zone |
| `CollapsiblePanelHeader.tsx` | — | Collapsible panel UI pattern | Queue, Events panels |
| `usePanelCollapse.ts` | hook | Panel collapse state management | All collapsible sections |

The Go backend already has all the API endpoints (status, workers, events, costs, actions). The SSE streaming endpoint `/api/forge/activity/stream` is production-tested. **No new backend work needed for Phase 1** — just a new React page that arranges existing data in the mission control layout.

## Backend API

### Read endpoints (state.db, read-only)
```
GET  /api/forge/status                  — daemon status + summary
GET  /api/forge/workers                 — active + recent workers
GET  /api/forge/workers/{id}            — single worker detail
GET  /api/forge/queue                   — ready beads, grouped by anvil
GET  /api/forge/prs                     — open PRs with full status
GET  /api/forge/events?limit=&type=&anvil=&from=&to=  — event log
GET  /api/forge/costs?period=today|week|month         — cost data
GET  /api/forge/costs/breakdown?period=&by=anvil|bead — detailed breakdown
GET  /api/forge/ingots?anvil=&status=   — bead lifecycle tracking
GET  /api/forge/anvils                  — anvil health summary
```

### Streaming endpoints (SSE)
```
GET  /api/forge/events/stream           — live event stream
GET  /api/forge/worker/{id}/stream      — live worker log stream
```

### Action endpoints (IPC commands)
```
POST /api/forge/action/refresh          — trigger immediate poll
POST /api/forge/action/kill             — kill a running worker
POST /api/forge/action/merge            — merge a PR
POST /api/forge/action/retry            — retry a stuck bead
POST /api/forge/action/run              — manually dispatch a bead
POST /api/forge/action/clarify          — mark bead as needing clarification
POST /api/forge/action/dismiss          — dismiss a needs-attention item
POST /api/forge/action/restart          — rebuild & restart forge daemon
POST /api/forge/action/pr/{action}      — quench/burnish/rebase/close PR
POST /api/forge/action/approve-as-is    — bypass warden, create PR
POST /api/forge/action/warden-rerun     — re-run warden review
POST /api/forge/action/force-smith      — push to another smith iteration
POST /api/forge/action/close-bead       — close a bead via bd
POST /api/forge/action/tag-bead         — add label to bead
POST /api/forge/action/append-notes     — add notes to bead
```

## Visual Design

- **Theme**: Dark mode (matches Hearth's aesthetic), with optional light mode
- **Colors**: 
  - Running workers: blue glow
  - Success/merged: green
  - Failed/errors: red
  - Needs attention: amber/yellow
  - Idle: dim gray
  - Crucible: purple
- **Typography**: Monospace for log output, system font for UI
- **Animations**: Subtle — pulse on new events, slide-in for new workers, fade for completed items
- **Worker panels**: Dark card with colored left border (phase color)

## Phase Plan

### Phase 1: The Forge Floor (live workers)
- Status bar + worker panels with live log streaming (SSE)
- Basic queue list (no drag-reorder yet)
- Kill worker action
- Mobile-responsive layout

### Phase 2: Pipeline + Actions  
- PR pipeline visualization with merge/retry/quench actions
- Needs attention panel with retry/clarify/dismiss
- Queue actions (run now, clarify)
- Confirmation dialogs

### Phase 3: Events + Costs
- Live event stream (SSE)
- Event filtering
- Cost dashboard with Recharts charts
- Daily limit indicator

### Phase 4: Detail Pages + Polish
- Worker detail page with full log
- Full event log page with search
- Cost breakdown page
- Ingot lifecycle tracking
- Anvil health page
- Keyboard shortcuts
- Push notification deep links

### Phase 5: Power Features
- Drag-to-reorder queue
- Event correlation (click to see full bead lifecycle)
- Cost alerts and budget configuration
- Worker phase timeline visualization
- Rebuild & restart button
- Multi-anvil dashboard filtering

## Why Mezzanine > Hearth

| | Hearth (TUI) | Current Dashboard | Mezzanine (Web) |
|--|--|--|--|
| See worker output | Last 3 lines | Full live stream (LiveActivity.tsx) | Per-worker streams in mission control grid |
| Merge a PR | `m` then confirm | Merge button with confirmation | One-click with full CI/review context inline |
| Check from phone | SSH + tmux + tiny screen | Works but layout is basic | Mobile-first mission control with touch actions |
| Cost trends | "$4.23 today" text | Basic cost display | Interactive Recharts with daily/weekly/monthly breakdown |
| PR status | "CI passing" text | Status badges | Full CI check list, review status, merge readiness in pipeline |
| Queue management | View only | Run/retry buttons | Drag to reorder, right-click context menu, priority badges |
| Deep links | No | No | `/mezzanine/worker/3`, `/mezzanine/pr/486` |
| Multiple viewers | One SSH session | Anyone with admin access | Same, but better layout for multiple monitors |
| Notifications | Must be watching | Push notifications | Push with deep links to specific worker/PR/bead |
| Event history | Last ~20 in panel | Scrollable event list | Full searchable log with filters + event correlation |
| Bead lifecycle | Not tracked visually | Not tracked | Pipeline visualization: Queue → Smith → Temper → Warden → PR → Merged |
| Layout | 3 fixed text columns | Single-page card layout | Configurable mission control grid, resizable zones |
