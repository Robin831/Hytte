category: Fixed
- **Fix Wordfeud login failing due to HTTP redirect** - Prevent Go HTTP client from following redirects on POST requests, which caused the request body to be silently dropped. Also add User-Agent header required by the Wordfeud API. (Hytte-t82n)
