category: Added
- **Bearer token auth for fit file upload** - The POST /api/training/upload endpoint now accepts an Authorization: Bearer token (configured via HYTTE_UPLOAD_TOKEN env var) in addition to session cookies, enabling automated local tools to upload .fit files without a browser session. (Hytte-tfut)
