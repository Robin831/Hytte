# Hytte Netatmo Dashboard Widget

**Status**: Planning
**Date**: 2026-03-28

---

## Overview

Add a small widget to the Hytte dashboard showing current readings from a Netatmo weather station — indoor/outdoor temperature, humidity, CO2, noise level.

## Netatmo API

Netatmo has an official REST API with OAuth2 authentication.

- **API docs**: https://dev.netatmo.com/apidocumentation
- **Endpoint**: `GET /api/getstationsdata` — returns all station data
- **Auth**: OAuth2 with refresh tokens (similar to Google Calendar)
- **Rate limit**: 50 requests per 10 seconds per user (generous)

### Scopes needed
- `read_station` — access to weather station data

### Data available per module
- **Indoor**: temperature, humidity, CO2, noise, pressure
- **Outdoor**: temperature, humidity
- **Rain gauge**: rain (mm), rain_24h
- **Wind gauge**: wind speed, gust, direction

## Implementation

### Backend: `internal/netatmo/`

Small package:
- **client.go** — OAuth2 client, token refresh, `GetStationsData()` call
- **handlers.go** — `GET /api/netatmo/current` returning current readings
- **models.go** — Station, Module, Reading structs

Token storage: reuse the same `google_tokens` pattern (or a generic `oauth_tokens` table) with encrypted storage.

Cache readings for 5 minutes (Netatmo stations update every ~5 min anyway).

### Frontend: Dashboard Widget

Small card on the Dashboard page:
```
┌─────────────────────────────┐
│ 🌡️ Home                     │
│                             │
│ Indoor   22.3°C  💧 45%    │
│          CO₂ 812 ppm       │
│ Outdoor  8.1°C   💧 78%    │
│                             │
│ Updated 2 min ago           │
└─────────────────────────────┘
```

Color-code CO2: green (<1000), yellow (1000-1500), red (>1500).
Temperature: blue if cold (<5°), green normal, orange hot (>25°).

### Setup Flow

1. User goes to Settings → Netatmo section
2. Click "Connect Netatmo" → OAuth2 redirect to Netatmo
3. Authorize → callback stores tokens
4. Widget appears on dashboard

### Feature Flag

`netatmo` — default false, enabled per user via Admin.

## Phase Plan

### Phase 1: Basic readings
- Netatmo OAuth2 setup + token persistence
- Fetch station data, display indoor/outdoor temp + humidity
- Dashboard widget
- Feature flag + settings connection UI

### Phase 2: Enhancements
- CO2, noise, pressure display
- Rain gauge data (if present)
- Historical charts (Netatmo API supports time ranges)
- Mini weather graph (24h temperature trend)

## Decisions

1. **Modules**: Indoor, outdoor, and wind (rain gauge not working). Show all modules that return data — gracefully skip offline ones.
2. **Sharing**: All family members see the widget using the admin's Netatmo connection. No per-user OAuth — one household, one station. Store the token under the admin user, serve data to all authenticated users.
3. **Historical data**: Yes — show a 24h temperature trend graph. Cache historical readings in SQLite but don't retain more than ~7 days. A nice sparkline or mini chart on the widget, expandable to a fuller view.
