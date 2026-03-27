# Hytte Skyss Bus Departures

**Status**: Planning
**Date**: 2026-03-26

---

## Overview

Show real-time bus departure information from Skyss (Bergen public transport) on a dedicated Hytte page. The user configures which stops/routes to watch, and Hytte displays upcoming departures.

## Approach: Embed vs API

### Option A: Embed the Skyss Widget

Skyss offers an embeddable departure display at `https://avgangsvisning.skyss.no/`. You configure your stops/routes on that site and get a URL to embed.

**Pros:**
- Zero maintenance — Skyss handles all the data
- Always up-to-date with Skyss's own real-time system
- Takes 5 minutes to set up

**Cons:**
- Styling is Skyss's, not Hytte's dark theme
- Limited customization
- iframe embedding can be janky on mobile
- If Skyss changes their embed, it breaks

**Implementation:**
```tsx
<iframe
  src="https://avgangsvisning.skyss.no/?config=YOUR_CONFIG_ID"
  className="w-full h-[600px] border-0 rounded-lg"
  title="Skyss Departures"
/>
```

### Option B: Use the Entur API (Recommended)

Skyss data flows through **Entur** (the national Norwegian transport data platform). Entur has a free, open GraphQL API that covers all Norwegian public transport — including Skyss.

**API**: `https://api.entur.io/journey-planner/v3/graphql`
**No API key required** — just set a custom `ET-Client-Name` header.
**Documentation**: `https://developer.entur.org/`

**Pros:**
- Full control over styling (matches Hytte dark theme)
- Real-time data (same source as Skyss's own displays)
- Can show multiple stops on one page
- Can add journey planning later
- Works for any Norwegian transport, not just Skyss

**Cons:**
- More work than an iframe
- Need to maintain stop IDs

**Recommendation**: Go with Option B (Entur API). The iframe is tempting but will look out of place in Hytte's dark theme. The Entur API is genuinely easy to use and free.

## Implementation Plan (Option B)

### 1. Configuration: Favorite Stops

Store the user's favorite stops in `user_preferences`:
```
key: "transit_stops"
value: JSON array of stop objects
```

```json
[
  { "id": "NSR:StopPlace:58191", "name": "Fantoft", "routes": ["6"] },
  { "id": "NSR:StopPlace:58366", "name": "Bergen busstasjon", "routes": [] }
]
```

When `routes` is empty, show all departures from that stop. When specified, filter to only those route numbers.

**Stop IDs**: Use Entur's Geocoder API to search for stops by name:
```
GET https://api.entur.io/geocoder/v1/autocomplete?text=Fantoft&layers=venue&categories=onstreetBus,busStation
```

### 2. Backend: Transit Package

Create `internal/transit/` with:

- **entur.go** — GraphQL client for Entur's journey planner API
  ```graphql
  query {
    stopPlace(id: "NSR:StopPlace:58191") {
      name
      estimatedCalls(numberOfDepartures: 10) {
        expectedDepartureTime
        destinationDisplay { frontText }
        serviceJourney {
          line { publicCode name transportMode }
        }
        realtime
      }
    }
  }
  ```

- **handlers.go** — HTTP handlers:
  - `GET /api/transit/departures?stops=NSR:StopPlace:58191,NSR:StopPlace:58366` — fetch upcoming departures
  - `GET /api/transit/search?q=Fantoft` — search for stops (proxies Entur Geocoder)
  - `GET /api/transit/settings` — get saved stops
  - `PUT /api/transit/settings` — save favorite stops

- **models.go** — Departure struct (line, destination, departure time, is_realtime, platform, delay_minutes)

**Caching**: Cache departures for 30 seconds. The Entur API is fast but no need to hammer it on every page load.

### 3. Frontend: TransitPage.tsx

**Layout:**
- One card per favorite stop
- Each card shows stop name and next 5-8 departures
- Departures show: route number (colored badge), destination, minutes until departure
- Real-time indicator (green dot if live, grey if scheduled)
- Auto-refresh every 30 seconds

**Example card:**
```
┌─────────────────────────────────┐
│ 🚌 Fantoft                      │
│                                 │
│  6   Bergen sentrum      3 min  │
│  6   Bergen sentrum     18 min  │
│  6   Birkelundstoppen   12 min  │
│  67  Lagunen             8 min  │
└─────────────────────────────────┘
```

**Settings panel** (expandable or separate route):
- Search box to find and add stops
- Drag to reorder favorite stops
- Per-stop route filter (only show specific lines)
- Remove stop button

**Recommendation**: Show "X min" for departures within 30 minutes, show the actual time (e.g., "14:35") for later departures. This matches how Norwegians read departure boards.

### 4. i18n

New namespace: `transit.json`
- `transit.title` = "Departures" / "Avganger" / "เวลาออกเดินทาง"
- `transit.minutes` = "min"
- `transit.addStop`, `transit.removeStop`, `transit.searchStops`
- `transit.realtime`, `transit.scheduled`
- `transit.noDepartures` = "No upcoming departures"

### 5. Feature Flag

Add `"transit": true` to feature defaults — simple utility, no reason to gate it.

### 6. Sidebar

Add nav item with `Bus` or `TrainFront` icon from Lucide, label "Departures" / "Avganger".

## Phase Plan

### Phase 1: Core departures display
- Entur API client (GraphQL)
- Hardcoded stops (your usual stops) — get it working first
- Frontend: departure cards with auto-refresh
- Feature flag + sidebar + i18n

### Phase 2: Stop management
- Stop search via Entur Geocoder
- Save/remove favorite stops in user_preferences
- Per-stop route filtering
- Settings UI

### Phase 3: Extras
- Journey planner (A→B with transfers)
- Departure notifications ("Bus 6 in 5 minutes")
- Widget for dashboard page (compact view of next departure per stop)

## Decisions

1. **Default stops**: Bjørndalsbakken and Olav Kyrres gate (filter to bus 3 and 3E westbound toward Vadmyra). Look up Entur stop IDs when creating beads.
2. **Approach**: Entur API only — skip the Skyss iframe embed.
3. **Visibility**: Managed via existing admin/feature flag system — no special access control needed.

