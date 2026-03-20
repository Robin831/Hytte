category: Fixed
- **Admin user can now see all feature-gated pages** - The /api/auth/me response returned features as a sibling of the user object, but the frontend only stored the user object, losing the features map. Now merges features into the user state so hasFeature() works correctly for all users. (Hytte-d74)
