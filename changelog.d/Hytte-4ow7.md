category: Added
- **Kiosk page** - Full-screen tablet display at `/kiosk` with clock, bus departures, weather strip, and sunrise/sunset. Reads `?token=` from URL query params and polls `/api/kiosk/data` every 30 seconds; falls back to mock data when no token is present. Rendered outside the main nav layout. (Hytte-4ow7)
