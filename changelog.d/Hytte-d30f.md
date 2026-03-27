category: Fixed
- **Transit departures now show real-time data** - Fixed two bugs causing 'No upcoming departures' for all stops: corrected the NSR stop IDs for Bjørndalsbakken (31927) and Olav Kyrres gate (30853), and fixed a malformed GraphQL query that placed the `quay` field inside `serviceJourney` instead of at the `estimatedCalls` level. (Hytte-d30f)
