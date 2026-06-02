category: Fixed
- **Pause transit auto-refresh when the tab is hidden** - The Transit page no longer polls the Entur departures endpoint while the browser tab is backgrounded, saving API budget and battery. When the tab becomes visible again it performs an immediate refresh and resumes the 30-second cadence. (Hytte-kc37)
