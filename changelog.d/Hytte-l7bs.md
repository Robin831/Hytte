category: Changed
- **Scanned-cards page stops polling when idle** - The scan list now only polls the scan API while a job is queued/processing or the Pending filter is active, and tears the interval down once everything settles — saving needless network, battery, and server load on the long-lived Needs review / Recently resolved tabs. (Hytte-l7bs)
