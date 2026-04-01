category: Fixed
- **Fix Wordfeud login failure** - Add trailing slash to the Wordfeud login API endpoint to prevent HTTP redirect from converting POST to GET and dropping the request body. (Hytte-t6r6)
