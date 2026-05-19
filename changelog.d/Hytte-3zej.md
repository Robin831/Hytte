category: Added
- **Pokémon page-scan upload endpoint** - POST /api/pokemon/scans/page accepts N cropped card images plus a JSON cells array, charges the daily scan cap atomically, and queues one pokemon_scan_pages parent with N pokemon_scan_jobs children that flow through the existing worker pipeline. (Hytte-3zej)
