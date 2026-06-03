category: Fixed
- **Encode credit-card query params** - Wrapped `credit_card_id`, `month`, and `account_id` query-string values in `encodeURIComponent` across all credit-card fetch sites so identifiers or months containing spaces, `&`, or `+` no longer produce malformed URLs that silently return no rows. (Hytte-4vqi)
