category: Added
- **Parent Challenge Management UI** - Added `/family/challenges` page for parents to create, edit, and delete challenges with a full form (title, description, type, target, star reward, date range, child enrollment). Active and past challenges are shown with per-participant completion status and inline add/remove participant controls. (Hytte-g9re)
- **Challenge participants API** - Added `GET /api/family/challenges/{id}/participants` endpoint returning enrolled children with their completion status, and `GET /api/family/challenges/participants` batch endpoint returning all participants grouped by challenge ID for efficient page load. (Hytte-g9re)
- **Backend test coverage** - Added duration-type progress and completion tests to the challenges test suite. (Hytte-g9re)
