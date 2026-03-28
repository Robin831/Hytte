category: Fixed
- **Family page now shows family info for child users** - Child users visiting /family previously saw an empty page. They now see a child-friendly view showing their parent's name, siblings (other linked children with avatar and nickname), star balance, and level for each family member. A new `GET /api/family/my-family` endpoint supports this view. (Hytte-4yhe)
