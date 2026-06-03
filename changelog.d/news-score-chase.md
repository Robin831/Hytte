category: Fixed
- **News relevance scores no longer vanish on refresh** - The feed now remembers scores client-side so a background revalidation never blanks them, and keeps refetching (bounded) until every article is scored instead of stopping after the first batch. The ranker also scores the whole feed in one pass rather than 40 at a time.
