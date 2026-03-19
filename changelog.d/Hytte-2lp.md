category: Security
- **Claude AI features restricted to admin users** - The Claude CLI settings, test endpoint, and UI section are now only accessible to admin users (is_admin flag on user record). The first registered user (ID 1) is automatically set as admin. Non-admin users receive a 403 error on Claude endpoints and do not see the Claude AI section in Settings. (Hytte-2lp)
