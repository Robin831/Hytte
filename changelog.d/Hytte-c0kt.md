category: Fixed
- **GitHub Actions response body too large** - Reduced workflow runs API query from per_page=100 to per_page=10 to prevent exceeding the 1MB response size limit. (Hytte-c0kt)
