category: Added
- **Pokémon card vision scan endpoint** - `POST /api/pokemon/scan` accepts a multipart image upload, runs it through Claude vision to identify the set and collector number, and returns matched card candidates with EUR/NOK pricing. Admin-gated and behind the `pokemon` feature flag. (Hytte-sud5)
