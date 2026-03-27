# Hytte Sky Watch — Stars, Planets & Moon

**Status**: Planning
**Date**: 2026-03-26

---

## Overview

A simple astronomical observation page: what's visible in the sky right now from your location? Which planets can you spot, where's the moon, what phase is it in? No telescope-grade precision needed — just a "look up and see" guide.

## Why This Is Fun to Build

Astronomical calculations are surprisingly doable in pure code — no API required for the basics. Moon phase, planet visibility, sunrise/sunset, and star positions can all be computed from well-known algorithms. The result is a page that works offline and never hits rate limits.

## What to Show

### 1. Moon Section

- **Current phase**: visual icon (🌑🌒🌓🌔🌕🌖🌗🌘) + name ("Voksende halvmåne" / "Waxing Crescent")
- **Illumination percentage**: "47% illuminated"
- **Moonrise / moonset times** for today
- **Phase calendar**: next 30 days showing phase progression (small row of moon icons)
- **Next full moon / new moon** dates

**Recommendation**: The moon phase calculation is a well-known algorithm (Jean Meeus, "Astronomical Algorithms"). It's ~30 lines of Go code with no external dependencies. No API needed.

### 2. Visible Planets

Show which planets are above the horizon right now (or tonight):

| Planet | Status | Direction | Altitude | Best viewing |
|--------|--------|-----------|----------|-------------|
| Venus  | Visible now ✨ | SW | 35° | Evening star |
| Mars   | Rises 22:15 | — | Below horizon | After midnight |
| Jupiter | Visible now | S | 42° | Until 01:30 |
| Saturn | Not visible | — | — | Too close to sun |

For each visible planet: compass direction (N/NE/E/SE/S/SW/W/NW), altitude above horizon, and approximate magnitude (brightness).

**How to compute**: Planet positions require ephemeris calculations. Two options:

**Option A: Simplified ephemeris (recommended for v1)**
Use the VSOP87 truncated series or simpler Keplerian elements. Accurate to ~1° for the naked-eye planets (Mercury through Saturn) — more than enough for "look that way."

**Option B: External API**
- **astronomy-api.com** — free tier, REST, returns planet positions
- **US Naval Observatory API** — free, official, but slower
- The Astronomy Engine library (`github.com/cosinekitty/astronomy`) is a Go port of well-tested C code — handles all solar system calculations.

**Recommendation**: Use the `astronomy` Go library by Don Cross (Cosinekitty). It's a single dependency with zero external API calls, handles all the orbital mechanics, and is used in production by multiple astronomy apps. It computes rise/set times, altitude/azimuth, moon phases, and planet positions.

### 3. Sun Section

- **Sunrise / sunset** times
- **Golden hour** start/end (good for photos!)
- **Civil / nautical / astronomical twilight** times
- **Day length** and how much longer/shorter than yesterday
- **Solar noon** time

### 4. Tonight's Highlights (the fun part)

A curated "what to look for tonight" section:

- "Venus is the bright 'star' in the southwest after sunset"
- "The Moon is near Jupiter tonight — look south around 21:00"
- "The ISS passes over at 20:47 — look NW to SE" (this one needs an API, optional)

**Recommendation**: Generate these from the computed positions. If two objects are within ~5° of each other, flag it as a conjunction. If a planet is at opposition (opposite the sun), note it as "at its brightest."

### 5. Sky Map (Optional, Phase 2)

A simple polar projection showing:
- Horizon circle
- Cardinal directions (N/S/E/W)
- Major constellation outlines (just the bright ones: Orion, Big Dipper, Cassiopeia)
- Planet positions as colored dots
- Moon position
- Current view rotated based on user's location and time

This is the most ambitious part and could be a Phase 2 item. A basic version using SVG/Canvas is doable without a heavy library.

## Data & Computation

### Location

Use the user's location from Hytte's existing weather/location system. If weather location is set, reuse it. Otherwise, default to Bergen (60.39°N, 5.32°E).

Store in user_preferences:
```
key: "sky_watch_location"
value: {"lat": 60.39, "lon": 5.32, "name": "Bergen"}
```

### Backend: `internal/skywatch/`

- **astronomy.go** — wrapper around the astronomy library
  - `GetMoonPhase(time, lat, lon) MoonInfo`
  - `GetPlanetPositions(time, lat, lon) []PlanetInfo`
  - `GetSunTimes(date, lat, lon) SunTimes`
  - `GetTonightHighlights(date, lat, lon) []Highlight`

- **handlers.go** — HTTP handlers:
  - `GET /api/skywatch/now` — current sky state (moon + planets + sun)
  - `GET /api/skywatch/moon?days=30` — moon phase calendar
  - `GET /api/skywatch/sun?date=2026-03-26` — sun times for a date

- **models.go** — MoonInfo, PlanetInfo, SunTimes, Highlight structs

**Recommendation**: All computation happens server-side in Go. The astronomy library is fast (microseconds per calculation). No caching needed — just compute on every request. This keeps the frontend simple (just render JSON) and ensures the data is always fresh.

### No External APIs Required (v1)

Everything except ISS passes can be computed locally:
- Moon phase: analytical formula
- Planet positions: VSOP87 or Keplerian elements (via astronomy library)
- Sun times: standard sunrise equation
- Conjunctions: angular distance between objects

The ISS thing is cool but optional — it needs the TLE (Two-Line Element) data from CelesTrak, which updates daily.

## Frontend: SkyWatchPage.tsx

### Layout

**Hero section**: current moon phase (large, centered, beautiful)
- Moon image or high-quality SVG with illumination
- Phase name + illumination %
- Next full moon countdown

**Sun card**: sunrise 🌅, sunset 🌇, day length, golden hour

**Planets grid**: one card per planet showing visibility status
- Visible: bright card with direction arrow
- Not visible: dimmed with "rises at XX:XX" or "not visible tonight"

**Tonight's highlights**: narrative text boxes with what to look for

**Moon calendar**: horizontal scrollable row of 30 days with tiny phase icons

### Design Notes

- **Dark theme is perfect** for a stargazing page — lean into it
- Use deep navy/black backgrounds with subtle star particles
- Planet colors: Mercury (grey), Venus (yellow), Mars (red), Jupiter (beige/orange), Saturn (gold)
- Moon: consider a CSS gradient or SVG that shows the actual illumination angle
- Subtle animations: stars twinkle, moon glow pulses

**Recommendation**: This page should feel different from the rest of Hytte — more contemplative, less dashboard-y. Think of it as the "lean back and look up" page. Minimal UI chrome, maximum sky.

## i18n

New namespace: `skywatch.json`
- Moon phases: "Nymåne" / "Voksende sigd" / "Første kvarter" / "Voksende halvmåne" / "Fullmåne" / etc.
- Planets: "Venus" (same in all languages), directions: "Sør" / "Nord" / etc.
- Times: use `Intl.DateTimeFormat` for locale-appropriate time formatting

Norwegian moon phase names are well-established — use them.

## Phase Plan

### Phase 1: Moon & Sun
- Moon phase calculation + display
- Moon rise/set times
- Sun rise/set + twilight times
- Phase calendar (30 days)
- Feature flag + sidebar + i18n

### Phase 2: Planets
- Planet position calculations (via astronomy library)
- Visibility determination (above horizon, not too close to sun)
- Planet cards with direction and altitude
- "Tonight's highlights" generator

### Phase 3: Sky Map
- Simple polar projection (SVG or Canvas)
- Moon + planet positions plotted
- Major constellation outlines
- Rotated to match observer's facing direction

### Phase 4: Extras
- ISS pass predictions (CelesTrak TLE data)
- Meteor shower calendar
- Light pollution indicator
- "Best viewing conditions" forecast (combines weather + moon brightness)
- Widget for dashboard (next moonrise, visible planets count)

## Technical Notes

### Go Astronomy Libraries

Best option: `github.com/cosinekitty/astronomy` (Astronomy Engine)
- Pure Go, no CGO
- Handles: rise/set, altitude/azimuth, moon phase, planet positions, eclipses, conjunctions
- Well-tested (ported from C reference implementation)
- MIT license
- One dependency to add to go.mod

### Performance

All calculations are O(1) and take microseconds. No database needed for this feature — everything is computed on the fly from the current time and location. The only persisted data is the user's preferred location (in user_preferences).

## Decisions

1. **Default location**: Home coordinates — 60.36091°N, 5.24056°E (Bjørndal area, Bergen). Fall back to these when no weather location is set.
2. **ISS pass predictions**: Yes — include in a later phase (requires CelesTrak TLE data).
3. **Sky map**: Nice-to-have, last phase. May warrant its own planning doc given the complexity (SVG/Canvas rendering, constellation data, rotation).
4. **Visibility**: Managed via existing admin/feature flag system.
5. **Aurora forecasts**: Yes — use NOAA solar wind data + Kp index. Great for Bergen's latitude (60°N is prime aurora territory on active nights).
