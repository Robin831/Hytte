category: Changed
- **Client-side image compression before upload** - Photos are now resized to max 1024px wide and re-encoded as JPEG at 85% quality using the Canvas API before being sent to the server, reducing upload size without any new dependencies. (Hytte-cts1)
