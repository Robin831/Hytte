category: Fixed
- **Allowance Chores/Payouts tabs show skeletons while loading** - The first visit to the Chores and Payouts tabs now renders skeleton placeholders while their data is in flight instead of a momentary blank list, matching the Extras and Bonuses tabs. This behavior is driven by the shared `useTabFetch` hook and is covered by a new regression test. (Hytte-nwe3)
