category: Fixed
- **Moon calendar honors the requested date** - The `/api/skywatch/moon` endpoint now accepts an optional `date=YYYY-MM-DD` query parameter and seeds the calendar from that day instead of always starting today, mirroring the `now` endpoint and keeping the two consistent. (Hytte-d3qy)
