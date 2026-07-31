category: Added
- **Recipe detail page with portion scaling** - Open a recipe to read its ingredients and method, rate it, log a cook, and edit every field, tag, ingredient and step. A portion control rescales the displayed amounts (rendering 1.5 dl as "1 1/2 dl") without ever touching the stored recipe, so saving an edit from a scaled view still writes the base quantities. Scaling rewrites only the amount inside each ingredient line, so prep notes and parentheticals such as "400 g cod, cubed" survive. (Hytte-wevzx)
- **Add missing ingredients to the grocery list** - Tick the ingredients you are out of and push them onto the grocery list in one action, with pending, success and error feedback and a count of the items that were already on the list. (Hytte-wevzx)
- **Create a recipe from the detail page** - "New recipe" and the URL importer now open a full editor where the parsed recipe can be reviewed and saved. (Hytte-wevzx)

category: Fixed
- **Loading a single recipe** - The recipe, save, rating and cook-log endpoints wrap their payload in a `recipe` field; the frontend read the envelope itself, so a recipe never rendered. (Hytte-wevzx)
