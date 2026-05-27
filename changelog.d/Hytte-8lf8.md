category: Fixed
- **Grocery list polling no longer goes silently stale after navigation or hot reload** - The polling effect now creates a fresh `AbortController` per 5-second tick instead of reusing a single mount-level controller, so cancellation cancels only the in-flight request and subsequent polls continue to succeed. (Hytte-8lf8)
