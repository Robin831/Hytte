category: Fixed
- **Saved news articles now persist across feed caps** - Bookmarked articles are served from their own encrypted snapshot store, so they survive the 48h feed age cap and 100-item truncation. Re-saving a churned article now refreshes all snapshot fields (source, source name, image, publish time), not just title/url/summary. (Hytte-6mxo)
