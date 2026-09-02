category: Added
- **Kiosk night-mode overrides in the token config** - Kiosk tokens can now set `dim`, `dim_start` and `dim_end` in their config; the `/api/kiosk/data` payload exposes them as a `dim` object alongside `sun`. Malformed values are ignored so the display falls back to its sun-driven default. (Hytte-lz3ba)
