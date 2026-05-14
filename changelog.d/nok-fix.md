category: Fixed
- **Norges Bank EUR/NOK fetch** - Switch from the deprecated `format=csv-no-utf8` URL parameter (which began returning HTTP 500 "Could not resolve delimiter 'utf8'" in May 2026) to plain `format=csv`. Same CSV shape, same parser, so no other changes needed. Restores NOK price display on the Pokémon collection page.
