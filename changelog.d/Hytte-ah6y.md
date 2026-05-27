category: Changed
- **Faster Credit Cards transaction list** - Virtualised the per-group transaction rows with `react-window` and memoised `GroupSection`/`TransactionItem` plus the per-group totals so the page stays smooth with 1000+ transactions. Assigning a single transaction now re-renders only the affected row instead of every sibling. (Hytte-ah6y)
