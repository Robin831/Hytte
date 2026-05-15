category: Fixed
- **Pokémon scanner collector-number match** - Claude returns the full printed format from the card face ("108/142"), but pokemontcg.io / our DB stores just the numerator ("108"). The lookup was doing an exact string compare and missing every modern card. Strip the "/<total>" suffix before lookup; variants like "025a/195" and promo formats like "SWSH123" still work correctly.
